package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/johns/vibe-vault/internal/help"
)

func main() {
	dir := "man"
	if len(os.Args) > 1 {
		dir = os.Args[1]
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "gen-man: %v\n", err)
		os.Exit(1)
	}

	date := time.Now().Format("2006-01-02")

	// Top-level man page
	if err := write(dir, "vv.1", help.FormatRoffTopLevel(help.TopLevel, help.Subcommands, date)); err != nil {
		fmt.Fprintf(os.Stderr, "gen-man: %v\n", err)
		os.Exit(1)
	}

	// Per-subcommand man pages
	for _, cmd := range help.Subcommands {
		filename := cmd.ManName() + ".1"
		if err := write(dir, filename, help.FormatRoff(cmd, date)); err != nil {
			fmt.Fprintf(os.Stderr, "gen-man: %v\n", err)
			os.Exit(1)
		}
	}

	// Hook sub-subcommand man pages
	for _, cmd := range help.HookSubcommands {
		filename := cmd.ManName() + ".1"
		if err := write(dir, filename, help.FormatRoff(cmd, date)); err != nil {
			fmt.Fprintf(os.Stderr, "gen-man: %v\n", err)
			os.Exit(1)
		}
	}
}

func write(dir, name, content string) error {
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	fmt.Printf("  %s\n", path)
	return nil
}
