// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/suykerbuyk/vibe-vault/internal/help"
	"github.com/suykerbuyk/vibe-vault/internal/staging"
	"github.com/suykerbuyk/vibe-vault/internal/vaultsync"
)

// runStaging is the dispatch entry for `vv staging <subcommand>`.
// Mirrors runVault / runZed shape: subcommand string switch, per-arm
// `wantsHelp` early-return, fatal on unknown.
func runStaging() {
	args := os.Args[2:]
	if len(args) == 0 || wantsHelp(args) {
		fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdStaging))
		return
	}

	switch args[0] {
	case "init":
		runStagingInit(args[1:])
	case "status":
		runStagingStatus(args[1:])
	case "path":
		runStagingPath(args[1:])
	case "gc":
		runStagingGc(args[1:])
	case "migrate":
		runStagingMigrate(args[1:])
	default:
		fatal("unknown staging subcommand: %s", args[0])
	}
}

// requireProject extracts the (single) positional project arg from a
// subcommand. Fatals when the argument is missing — every staging
// subcommand is per-project, so empty input is operator error.
func requireProject(usage string, args []string) string {
	for _, a := range args {
		if a == "" || a[0] == '-' {
			continue
		}
		return a
	}
	fatal("usage: %s", usage)
	return ""
}

func runStagingInit(args []string) {
	if wantsHelp(args) {
		fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdStagingInit))
		return
	}
	project := requireProject("vv staging init <project>", args)
	if err := staging.Init(project); err != nil {
		fatal("staging init: %v", err)
	}
	dir, err := staging.Path(project)
	if err != nil {
		fatal("staging path: %v", err)
	}
	fmt.Printf("staging: project %q initialized at %s\n", project, dir)
}

func runStagingPath(args []string) {
	if wantsHelp(args) {
		fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdStagingPath))
		return
	}
	project := requireProject("vv staging path <project>", args)
	dir, err := staging.Path(project)
	if err != nil {
		fatal("staging path: %v", err)
	}
	fmt.Println(dir)
}

func runStagingStatus(args []string) {
	if wantsHelp(args) {
		fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdStagingStatus))
		return
	}
	project := requireProject("vv staging status <project>", args)
	dir, err := staging.Path(project)
	if err != nil {
		fatal("staging path: %v", err)
	}
	info, statErr := os.Stat(filepath.Join(dir, ".git", "HEAD"))
	if os.IsNotExist(statErr) {
		fmt.Printf("staging: project %q NOT initialized at %s\n", project, dir)
		fmt.Println("hint: run `vv staging init " + project + "`")
		return
	}
	if statErr != nil {
		fatal("stat .git/HEAD: %v", statErr)
	}
	fmt.Printf("staging: project %q initialized at %s\n", project, dir)
	fmt.Printf("  .git/HEAD: %s\n", info.ModTime().Format(time.RFC3339))

	porcelain, err := vaultsync.GitCommand(dir, 5*time.Second, "status", "--porcelain")
	if err != nil {
		fatal("git status: %v", err)
	}
	if porcelain == "" {
		fmt.Println("  worktree:  clean")
	} else {
		fmt.Println("  worktree:  dirty")
		fmt.Println(porcelain)
	}

	// Last commit may be absent on a brand-new repo (init writes no commit).
	lastCommit, err := vaultsync.GitCommand(dir, 5*time.Second, "log", "-1", "--format=%cI %h %s")
	if err != nil {
		fmt.Println("  last commit: (none)")
		return
	}
	fmt.Printf("  last commit: %s\n", lastCommit)
}

func runStagingGc(args []string) {
	if wantsHelp(args) {
		fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdStagingGc))
		return
	}
	project := requireProject("vv staging gc <project>", args)
	dir, err := staging.Path(project)
	if err != nil {
		fatal("staging path: %v", err)
	}
	out, err := vaultsync.GitCommand(dir, 30*time.Second, "gc", "--auto")
	if err != nil {
		fatal("git gc: %v", err)
	}
	if out != "" {
		fmt.Println(out)
	}
	fmt.Printf("staging: gc complete for %s\n", dir)
}

func runStagingMigrate(args []string) {
	if wantsHelp(args) {
		fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdStagingMigrate))
		return
	}
	cfg := mustLoadConfig()
	project := flagValue(args, "--project")
	allProjects := hasFlag(args, "--all-projects")
	if project == "" && !allProjects {
		fatal("usage: vv staging migrate [--all-projects | --project <p>]")
	}
	if project != "" && allProjects {
		fatal("vv staging migrate: --project and --all-projects are mutually exclusive")
	}

	results, err := staging.Migrate(staging.MigrateOptions{
		VaultPath:   cfg.VaultPath,
		Project:     project,
		AllProjects: allProjects,
	})
	if err != nil {
		// Print partial progress before exiting; operator needs to see
		// which projects landed before the failure to decide on rollback.
		for _, r := range results {
			printMigrateResult(r)
		}
		fatal("staging migrate: %v", err)
	}
	for _, r := range results {
		printMigrateResult(r)
	}
}

func printMigrateResult(r staging.MigrateResult) {
	if len(r.Moved) == 0 {
		fmt.Printf("staging migrate: %s — no flat-layout sessions; skipped\n", r.Project)
		return
	}
	fmt.Printf("staging migrate: %s — %d notes archived, index entries fixed: %d (commit %s)\n",
		r.Project, len(r.Moved), r.IndexFixed, shortMigrationSHA(r.CommitSHA))
}

// shortMigrationSHA truncates a SHA to the conventional 7 chars,
// preserving the empty-string sentinel for the no-op path so the
// caller can still print "commit (none)" cleanly.
func shortMigrationSHA(sha string) string {
	if sha == "" {
		return "(none)"
	}
	if len(sha) >= 7 {
		return sha[:7]
	}
	return sha
}
