package archive

import (
	"os"
	"path/filepath"
	"testing"
)

const testSessionID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

func TestArchiveRoundTrip(t *testing.T) {
	srcDir := t.TempDir()
	archiveDir := t.TempDir()

	// Create a source transcript
	original := `{"type":"summary","session_id":"test"}` + "\n" +
		`{"type":"human","message":"hello"}` + "\n" +
		`{"type":"assistant","message":"world"}` + "\n"

	srcPath := filepath.Join(srcDir, testSessionID+".jsonl")
	if err := os.WriteFile(srcPath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	// Archive
	archPath, err := Archive(srcPath, archiveDir)
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}

	// Verify archive exists and is smaller
	srcInfo, _ := os.Stat(srcPath)
	archInfo, _ := os.Stat(archPath)
	if archInfo.Size() >= srcInfo.Size() {
		t.Logf("warning: archive (%d) not smaller than source (%d) — small test data",
			archInfo.Size(), srcInfo.Size())
	}

	// Decompress
	tmpPath, cleanup, err := Decompress(archPath)
	if err != nil {
		t.Fatalf("Decompress: %v", err)
	}
	defer cleanup()

	// Verify contents match
	decompressed, err := os.ReadFile(tmpPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(decompressed) != original {
		t.Errorf("decompressed content mismatch\ngot:  %q\nwant: %q", string(decompressed), original)
	}
}

func TestIsArchived(t *testing.T) {
	archiveDir := t.TempDir()

	if IsArchived(testSessionID, archiveDir) {
		t.Error("should not be archived yet")
	}

	// Create a fake archive file
	path := ArchivePath(testSessionID, archiveDir)
	if err := os.WriteFile(path, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	if !IsArchived(testSessionID, archiveDir) {
		t.Error("should be archived now")
	}
}

func TestArchivePath(t *testing.T) {
	got := ArchivePath("abc-123", "/vault/.vibe-vault/archive")
	want := "/vault/.vibe-vault/archive/abc-123.jsonl.zst"
	if got != want {
		t.Errorf("ArchivePath = %q, want %q", got, want)
	}
}
