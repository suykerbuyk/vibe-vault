// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/suykerbuyk/vibe-vault/internal/config"
	"github.com/suykerbuyk/vibe-vault/internal/vaultsync"
)

// runVaultRecover dispatches the `vv vault recover` subcommand variants:
//
//	vv vault recover                   list candidates from the past 7 days
//	vv vault recover --days N          extend the recovery window
//	vv vault recover --show <sha>      run `git show <sha>`
//	vv vault recover --diff <sha> -- <path>
//	                                   side-by-side `git show <sha>:<path>`
//	                                   and `git show HEAD:<path>`
func runVaultRecover(cfg config.Config, args []string) {
	// Pre-flight: --show takes precedence over --diff which takes
	// precedence over the default listing path. The flags carry their
	// own SHAs so the listing default doesn't fight them.
	if showSHA := flagValue(args, "--show"); showSHA != "" {
		runVaultRecoverShow(cfg.VaultPath, showSHA)
		return
	}
	if diffSHA := flagValue(args, "--diff"); diffSHA != "" {
		path := positionalAfterDoubleDash(args)
		if path == "" {
			fatal("vault recover --diff requires: --diff <sha> -- <path>")
		}
		runVaultRecoverDiff(cfg.VaultPath, diffSHA, path)
		return
	}

	days := 7
	if d := flagValue(args, "--days"); d != "" {
		n, err := strconv.Atoi(d)
		if err != nil {
			fatal("vault recover: --days requires an integer, got %q", d)
		}
		days = n
	}

	candidates, err := vaultsync.Recover(cfg.VaultPath, days)
	if err != nil {
		fatal("vault recover: %v", err)
	}
	if len(candidates) == 0 {
		fmt.Printf("vault: no recovery candidates in the past %d day(s)\n", days)
		return
	}

	now := time.Now()
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "AGE\tSHA\tSUBJECT\tFILES")
	for _, c := range candidates {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
			humanAge(now, c.CommittedAt),
			shortSHA(c.SHA),
			truncate(c.Subject, 60),
			strings.Join(c.Files, ", "),
		)
	}
	_ = tw.Flush()
	fmt.Println()
	fmt.Println("Inspect content: vv vault recover --show <sha>")
	fmt.Println("Side-by-side:    vv vault recover --diff <sha> -- <path>")
}

func runVaultRecoverShow(vaultPath, sha string) {
	cmd := exec.CommandContext(context.Background(), "git", "-C", vaultPath, "show", sha)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fatal("vault recover --show %s: %v", sha, err)
	}
}

func runVaultRecoverDiff(vaultPath, sha, path string) {
	dropped, derr := gitShowFile(vaultPath, sha+":"+path)
	if derr != nil {
		fatal("vault recover --diff: git show %s:%s: %v", sha, path, derr)
	}
	head, herr := gitShowFile(vaultPath, "HEAD:"+path)
	if herr != nil {
		// HEAD may genuinely lack the path (e.g., the file was deleted
		// at HEAD); show the dropped side and surface the absence.
		head = "(file does not exist at HEAD)"
	}

	fmt.Printf("=== Dropped (%s:%s) ===\n", shortSHA(sha), path)
	fmt.Println(dropped)
	fmt.Printf("\n=== HEAD:%s ===\n", path)
	fmt.Println(head)
}

func gitShowFile(vaultPath, ref string) (string, error) {
	cmd := exec.CommandContext(context.Background(), "git", "-C", vaultPath, "show", ref)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s: %w", strings.TrimSpace(string(out)), err)
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// shortSHA truncates a git SHA to 7 chars for breadcrumb display.
func shortSHA(sha string) string {
	if len(sha) <= 7 {
		return sha
	}
	return sha[:7]
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 1 {
		return s[:max]
	}
	return s[:max-1] + "…"
}

func humanAge(now, then time.Time) string {
	if then.IsZero() {
		return "?"
	}
	d := now.Sub(then)
	switch {
	case d < time.Minute:
		return "<1m"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// positionalAfterDoubleDash returns the first positional argument after
// a literal "--" separator in args, or "" if none.
func positionalAfterDoubleDash(args []string) string {
	for i, a := range args {
		if a == "--" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}
