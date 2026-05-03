// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/suykerbuyk/vibe-vault/internal/help"
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

	date := commitDate()

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

	// Context sub-subcommand man pages
	for _, cmd := range help.ContextSubcommands {
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

	// Config sub-subcommand man pages
	for _, cmd := range help.ConfigSubcommands {
		filename := cmd.ManName() + ".1"
		if err := write(dir, filename, help.FormatRoff(cmd, date)); err != nil {
			fmt.Fprintf(os.Stderr, "gen-man: %v\n", err)
			os.Exit(1)
		}
	}

	// Staging sub-subcommand man pages
	for _, cmd := range help.StagingSubcommands {
		filename := cmd.ManName() + ".1"
		if err := write(dir, filename, help.FormatRoff(cmd, date)); err != nil {
			fmt.Fprintf(os.Stderr, "gen-man: %v\n", err)
			os.Exit(1)
		}
	}
}

// commitDate returns the committer date of HEAD in YYYY-MM-DD format.
// This makes man page output deterministic for a given commit, avoiding
// spurious git diffs from date/version changes on every rebuild.
func commitDate() string {
	out, err := exec.Command("git", "log", "-1", "--format=%cd", "--date=short").Output()
	if err == nil {
		if d := strings.TrimSpace(string(out)); d != "" {
			return d
		}
	}
	return time.Now().Format("2006-01-02")
}

func write(dir, name, content string) error {
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	fmt.Printf("  %s\n", path)
	return nil
}
