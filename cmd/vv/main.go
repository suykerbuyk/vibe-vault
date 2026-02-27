package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/johns/vibe-vault/internal/archive"
	"github.com/johns/vibe-vault/internal/config"
	"github.com/johns/vibe-vault/internal/discover"
	"github.com/johns/vibe-vault/internal/hook"
	"github.com/johns/vibe-vault/internal/index"
	"github.com/johns/vibe-vault/internal/scaffold"
	"github.com/johns/vibe-vault/internal/session"
)

const version = "0.3.0"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "init":
		runInit()

	case "hook":
		cfg := mustLoadConfig()
		event := flagValue(os.Args[2:], "--event")
		if err := hook.Handle(cfg, event); err != nil {
			fatal("%v", err)
		}

	case "process":
		cfg := mustLoadConfig()
		if len(os.Args) < 3 {
			fatal("usage: vv process <transcript.jsonl>")
		}
		path := os.Args[2]
		result, err := session.Capture(session.CaptureOpts{TranscriptPath: path}, cfg)
		if err != nil {
			fatal("process: %v", err)
		}
		if result.Skipped {
			fmt.Printf("skipped: %s\n", result.Reason)
		} else {
			fmt.Printf("created: %s (%s)\n", result.NotePath, result.Title)
		}

	case "index":
		runIndex()

	case "backfill":
		runBackfill()

	case "archive":
		runArchive()

	case "reprocess":
		runReprocess()

	case "version":
		fmt.Printf("vv v%s (vibe-vault)\n", version)

	case "help", "--help", "-h":
		usage()

	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		usage()
		os.Exit(1)
	}
}

func runInit() {
	args := os.Args[2:]
	gitInit := hasFlag(args, "--git")
	args = removeFlag(args, "--git")

	target := "./vibe-vault"
	if len(args) > 0 {
		target = args[0]
	}

	absTarget, err := filepath.Abs(target)
	if err != nil {
		fatal("resolve path: %v", err)
	}

	fmt.Printf("Creating vault at %s\n", absTarget)

	if err := scaffold.Init(absTarget, scaffold.Options{GitInit: gitInit}); err != nil {
		fatal("init: %v", err)
	}

	cfgPath, err := config.WriteDefault(absTarget)
	if err != nil {
		fatal("write config: %v", err)
	}

	fmt.Println("\nDone! Next steps:")
	fmt.Printf("  1. Open %s in Obsidian\n", absTarget)
	fmt.Println("  2. Install community plugins: Dataview, Templater")
	fmt.Println("  3. Add hook to ~/.claude/settings.json:")
	fmt.Println(`     {"hooks": {"SessionEnd": [{"matcher": "", "hooks": [{"type": "command", "command": "vv hook"}]}]}}`)
	fmt.Printf("\nConfig written to %s\n", cfgPath)
}

func runIndex() {
	cfg := mustLoadConfig()

	idx, count, err := index.Rebuild(cfg.SessionsDir(), cfg.StateDir())
	if err != nil {
		fatal("index: %v", err)
	}

	if err := idx.Save(); err != nil {
		fatal("save index: %v", err)
	}

	fmt.Printf("indexed %d sessions\n", count)

	// Generate per-project context documents
	projects := idx.Projects()
	for _, project := range projects {
		doc := idx.ProjectContext(project)
		dir := filepath.Join(cfg.SessionsDir(), project)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Printf("warning: create dir for %s: %v", project, err)
			continue
		}
		path := filepath.Join(dir, "_context.md")
		if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
			log.Printf("warning: write context for %s: %v", project, err)
			continue
		}
		fmt.Printf("  context: %s\n", filepath.Join("Sessions", project, "_context.md"))
	}
}

func defaultTranscriptDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "projects")
}

func runBackfill() {
	cfg := mustLoadConfig()

	basePath := defaultTranscriptDir()
	args := os.Args[2:]
	if len(args) > 0 && !hasFlag(args, "--") {
		basePath = args[0]
	}

	if basePath == "" {
		fatal("cannot determine transcript directory")
	}

	fmt.Printf("Discovering transcripts in %s\n", basePath)

	transcripts, err := discover.Discover(basePath)
	if err != nil {
		fatal("discover: %v", err)
	}

	fmt.Printf("Found %d transcripts\n", len(transcripts))

	idx, err := index.Load(cfg.StateDir())
	if err != nil {
		fatal("load index: %v", err)
	}

	var processed, skipped, patched, errors int
	for _, tf := range transcripts {
		if idx.Has(tf.SessionID) {
			// Backfill TranscriptPath on existing entries that lack it
			entry := idx.Entries[tf.SessionID]
			if entry.TranscriptPath == "" {
				entry.TranscriptPath = tf.Path
				idx.Entries[tf.SessionID] = entry
				patched++
			}
			skipped++
			continue
		}

		result, err := session.Capture(session.CaptureOpts{
			TranscriptPath: tf.Path,
		}, cfg)
		if err != nil {
			log.Printf("error processing %s: %v", tf.SessionID, err)
			errors++
			continue
		}

		if result.Skipped {
			skipped++
			continue
		}

		processed++
		fmt.Printf("  %s → %s\n", result.Project, result.NotePath)

		// Reload index since Capture saved it
		idx, _ = index.Load(cfg.StateDir())
	}

	// Save index if we patched TranscriptPaths
	if patched > 0 {
		if err := idx.Save(); err != nil {
			log.Printf("warning: could not save index: %v", err)
		}
	}

	fmt.Printf("\nprocessed: %d, skipped: %d (already indexed or trivial), errors: %d\n",
		processed, skipped, errors)
	if patched > 0 {
		fmt.Printf("patched: %d (added transcript paths to existing entries)\n", patched)
	}
}

func runArchive() {
	cfg := mustLoadConfig()
	archiveDir := filepath.Join(cfg.StateDir(), "archive")

	idx, err := index.Load(cfg.StateDir())
	if err != nil {
		fatal("load index: %v", err)
	}

	var archived, skipped int
	var totalSrc, totalArch int64

	for _, entry := range idx.Entries {
		transcriptPath := entry.TranscriptPath

		// Fallback: try to discover if no TranscriptPath stored
		if transcriptPath == "" {
			defaultDir := defaultTranscriptDir()
			if defaultDir != "" {
				found, err := discover.FindBySessionID(defaultDir, entry.SessionID)
				if err == nil {
					transcriptPath = found
				}
			}
		}

		if transcriptPath == "" {
			skipped++
			continue
		}

		if archive.IsArchived(entry.SessionID, archiveDir) {
			skipped++
			continue
		}

		srcInfo, err := os.Stat(transcriptPath)
		if err != nil {
			skipped++
			continue
		}

		archPath, err := archive.Archive(transcriptPath, archiveDir)
		if err != nil {
			log.Printf("error archiving %s: %v", entry.SessionID, err)
			continue
		}

		archInfo, _ := os.Stat(archPath)
		totalSrc += srcInfo.Size()
		totalArch += archInfo.Size()
		archived++
	}

	fmt.Printf("archived: %d (%s → %s), skipped: %d\n",
		archived, humanBytes(totalSrc), humanBytes(totalArch), skipped)
}

func runReprocess() {
	cfg := mustLoadConfig()
	archiveDir := filepath.Join(cfg.StateDir(), "archive")
	projectFilter := flagValue(os.Args[2:], "--project")

	idx, err := index.Load(cfg.StateDir())
	if err != nil {
		fatal("load index: %v", err)
	}

	var processed, skipped, errors int
	affectedProjects := make(map[string]bool)

	for _, entry := range idx.Entries {
		if projectFilter != "" && entry.Project != projectFilter {
			continue
		}

		// Locate transcript (try in order)
		transcriptPath := ""
		var cleanup func()

		// 1. Original location
		if entry.TranscriptPath != "" {
			if _, err := os.Stat(entry.TranscriptPath); err == nil {
				transcriptPath = entry.TranscriptPath
			}
		}

		// 2. Archive
		if transcriptPath == "" {
			ap := archive.ArchivePath(entry.SessionID, archiveDir)
			if _, err := os.Stat(ap); err == nil {
				tmpPath, tmpCleanup, err := archive.Decompress(ap)
				if err == nil {
					transcriptPath = tmpPath
					cleanup = tmpCleanup
				}
			}
		}

		// 3. Fallback scan
		if transcriptPath == "" {
			defaultDir := defaultTranscriptDir()
			if defaultDir != "" {
				found, err := discover.FindBySessionID(defaultDir, entry.SessionID)
				if err == nil {
					transcriptPath = found
				}
			}
		}

		if transcriptPath == "" {
			log.Printf("warning: transcript not found for %s", entry.SessionID)
			skipped++
			continue
		}

		result, err := session.Capture(session.CaptureOpts{
			TranscriptPath: transcriptPath,
			Force:          true,
		}, cfg)

		if cleanup != nil {
			cleanup()
		}

		if err != nil {
			log.Printf("error reprocessing %s: %v", entry.SessionID, err)
			errors++
			continue
		}

		if result.Skipped {
			skipped++
			continue
		}

		processed++
		affectedProjects[result.Project] = true
		fmt.Printf("  %s → %s\n", result.Project, result.NotePath)
	}

	// Regenerate context docs for affected projects
	if len(affectedProjects) > 0 {
		idx, _ = index.Load(cfg.StateDir())
		for project := range affectedProjects {
			doc := idx.ProjectContext(project)
			dir := filepath.Join(cfg.SessionsDir(), project)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				log.Printf("warning: create dir for %s: %v", project, err)
				continue
			}
			path := filepath.Join(dir, "_context.md")
			if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
				log.Printf("warning: write context for %s: %v", project, err)
				continue
			}
			fmt.Printf("  context: %s\n", filepath.Join("Sessions", project, "_context.md"))
		}
	}

	fmt.Printf("\nreprocessed: %d, skipped: %d, errors: %d\n", processed, skipped, errors)
}

func humanBytes(b int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
	)
	switch {
	case b >= MB:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(MB))
	case b >= KB:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(KB))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `vv v%s — vibe-vault session capture

Usage:
  vv init [path] [--git]       Create a new vault (default: ./vibe-vault)
  vv hook [--event <name>]     Hook mode (reads stdin from Claude Code)
  vv process <file.jsonl>      Process a single transcript file
  vv index                     Rebuild session index from notes
  vv backfill [path]           Discover and process historical transcripts
  vv archive                   Compress transcripts into vault archive
  vv reprocess [--project X]   Re-generate notes from transcripts
  vv version                   Print version
  vv help                      Show this help

Hook integration (settings.json):
  {"type": "command", "command": "vv hook"}

Configuration: ~/.config/vibe-vault/config.toml
`, version)
}

func mustLoadConfig() config.Config {
	cfg, err := config.Load()
	if err != nil {
		fatal("load config: %v", err)
	}
	return cfg
}

func flagValue(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

func removeFlag(args []string, flag string) []string {
	var out []string
	for _, a := range args {
		if a != flag {
			out = append(out, a)
		}
	}
	return out
}

func fatal(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "vv: "+format+"\n", args...)
	os.Exit(1)
}
