package archive

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/klauspost/compress/zstd"
)

// Archive compresses srcPath into archiveDir/{session-id}.jsonl.zst.
// Returns the archive path.
func Archive(srcPath, archiveDir string) (string, error) {
	sessionID := extractSessionID(srcPath)
	if sessionID == "" {
		return "", fmt.Errorf("cannot extract session ID from %s", srcPath)
	}

	destPath := ArchivePath(sessionID, archiveDir)

	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		return "", fmt.Errorf("create archive dir: %w", err)
	}

	src, err := os.Open(srcPath)
	if err != nil {
		return "", fmt.Errorf("open source: %w", err)
	}
	defer src.Close()

	dest, err := os.Create(destPath)
	if err != nil {
		return "", fmt.Errorf("create archive: %w", err)
	}
	defer dest.Close()

	encoder, err := zstd.NewWriter(dest)
	if err != nil {
		return "", fmt.Errorf("create zstd encoder: %w", err)
	}

	if _, err := io.Copy(encoder, src); err != nil {
		encoder.Close()
		return "", fmt.Errorf("compress: %w", err)
	}

	if err := encoder.Close(); err != nil {
		return "", fmt.Errorf("finalize compression: %w", err)
	}

	return destPath, nil
}

// Decompress decompresses archivePath to a temp file.
// Returns the temp file path and a cleanup function the caller must defer.
func Decompress(archivePath string) (string, func(), error) {
	src, err := os.Open(archivePath)
	if err != nil {
		return "", nil, fmt.Errorf("open archive: %w", err)
	}
	defer src.Close()

	decoder, err := zstd.NewReader(src)
	if err != nil {
		return "", nil, fmt.Errorf("create zstd decoder: %w", err)
	}
	defer decoder.Close()

	tmp, err := os.CreateTemp("", "vv-decompress-*.jsonl")
	if err != nil {
		return "", nil, fmt.Errorf("create temp file: %w", err)
	}

	if _, err := io.Copy(tmp, decoder); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", nil, fmt.Errorf("decompress: %w", err)
	}

	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return "", nil, fmt.Errorf("close temp: %w", err)
	}

	cleanup := func() { os.Remove(tmp.Name()) }
	return tmp.Name(), cleanup, nil
}

// IsArchived returns true if an archive file exists for the given session ID.
func IsArchived(sessionID, archiveDir string) bool {
	_, err := os.Stat(ArchivePath(sessionID, archiveDir))
	return err == nil
}

// ArchivePath returns the deterministic archive path for a session ID.
func ArchivePath(sessionID, archiveDir string) string {
	return filepath.Join(archiveDir, sessionID+".jsonl.zst")
}

func extractSessionID(path string) string {
	base := filepath.Base(path)
	if strings.HasSuffix(base, ".jsonl") {
		return strings.TrimSuffix(base, ".jsonl")
	}
	if strings.HasSuffix(base, ".jsonl.zst") {
		return strings.TrimSuffix(base, ".jsonl.zst")
	}
	return ""
}
