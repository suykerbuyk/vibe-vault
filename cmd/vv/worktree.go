// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/suykerbuyk/vibe-vault/internal/help"
	"github.com/suykerbuyk/vibe-vault/internal/lockfile"
	"github.com/suykerbuyk/vibe-vault/internal/worktreegc"
)

// runWorktree dispatches `vv worktree <subcommand>`. Currently only `gc`
// is supported; the indirection leaves room for future worktree
// management subcommands without re-shaping cmd/vv/main.go's top-level
// switch.
func runWorktree(args []string) {
	if len(args) > 0 {
		switch args[0] {
		case "gc":
			if wantsHelp(args[1:]) {
				fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdWorktreeGc))
				return
			}
			worktreeGC(args[1:])
			return
		}
	}

	if wantsHelp(args) {
		fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdWorktree))
		return
	}

	fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdWorktree))
	os.Exit(1)
}

// worktreeGC parses flags for `vv worktree gc` and invokes
// worktreegc.Run on the current working directory's repo. Output is
// human-readable by default, JSON with --json.
func worktreeGC(args []string) {
	fs := flag.NewFlagSet("worktree gc", flag.ContinueOnError)
	// Suppress the default error/usage emission; we want our own routing.
	fs.SetOutput(os.Stderr)

	var (
		dryRun           bool
		jsonOut          bool
		candidateParents string
		forceUncaptured  bool
	)
	fs.BoolVar(&dryRun, "dry-run", false, "report would-reap actions without destructive changes")
	fs.BoolVar(&jsonOut, "json", false, "emit Result as indented JSON to stdout")
	fs.StringVar(&candidateParents, "candidate-parents", "",
		"CSV of candidate parent branches for capture verification (default: resolved at runtime)")
	fs.BoolVar(&forceUncaptured, "force-uncaptured", false,
		"reap even when the worktree branch contains commits not present on any candidate parent")

	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}

	repoPath, err := os.Getwd()
	if err != nil {
		fatal("worktree gc: getwd: %v", err)
	}

	opts := worktreegc.Options{
		DryRun:           dryRun,
		ForceUncaptured:  forceUncaptured,
		CandidateParents: parseCandidateParentsCSV(candidateParents),
	}

	res, err := worktreegc.Run(repoPath, opts)
	if err != nil {
		if errors.Is(err, lockfile.ErrLocked) {
			fmt.Fprintf(os.Stderr, "vv: worktree gc: another invocation in progress\n")
			os.Exit(2)
		}
		fmt.Fprintf(os.Stderr, "vv: worktree gc: %v\n", err)
		os.Exit(2)
	}

	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(res); err != nil {
			fatal("worktree gc: encode JSON: %v", err)
		}
		return
	}

	for _, a := range res.Actions {
		fmt.Printf("%s\t%s\t%s\tpid=%d\t%s\n",
			a.Verdict,
			a.Marker.BranchName,
			a.Marker.Harness,
			a.Marker.PID,
			a.Detail,
		)
	}
	fmt.Printf("reaped=%d alive=%d skipped=%d errors=%d\n",
		res.Reaped, res.Alive, res.Skipped, res.Errors)
}

// parseCandidateParentsCSV parses a comma-separated branch list per
// v5-L2 rules: trim whitespace, drop empty entries, dedupe
// case-sensitively (git refs are case-sensitive). Returns nil when
// every entry is empty so worktreegc.Run falls back to the
// default-branch resolver.
func parseCandidateParentsCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	seen := make(map[string]struct{}, len(parts))
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t == "" {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
