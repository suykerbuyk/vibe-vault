// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"context"
	"os/signal"
	"sync"
	"syscall"

	"github.com/suykerbuyk/vibe-vault/internal/archive"
	"github.com/suykerbuyk/vibe-vault/internal/check"
	"github.com/suykerbuyk/vibe-vault/internal/config"
	vvcontext "github.com/suykerbuyk/vibe-vault/internal/context"
	"github.com/suykerbuyk/vibe-vault/internal/discover"
	"github.com/suykerbuyk/vibe-vault/internal/effectiveness"
	"github.com/suykerbuyk/vibe-vault/internal/friction"
	"github.com/suykerbuyk/vibe-vault/internal/help"
	"github.com/suykerbuyk/vibe-vault/internal/hook"
	"github.com/suykerbuyk/vibe-vault/internal/identity"
	"github.com/suykerbuyk/vibe-vault/internal/index"
	"github.com/suykerbuyk/vibe-vault/internal/inject"
	"github.com/suykerbuyk/vibe-vault/internal/llm"
	"github.com/suykerbuyk/vibe-vault/internal/mcp"
	"github.com/suykerbuyk/vibe-vault/internal/memory"
	"github.com/suykerbuyk/vibe-vault/internal/meta"
	"github.com/suykerbuyk/vibe-vault/internal/scaffold"
	"github.com/suykerbuyk/vibe-vault/internal/session"
	"github.com/suykerbuyk/vibe-vault/internal/stats"
	"github.com/suykerbuyk/vibe-vault/internal/templates"
	"github.com/suykerbuyk/vibe-vault/internal/trends"
	"github.com/suykerbuyk/vibe-vault/internal/vaultsync"
	"github.com/suykerbuyk/vibe-vault/internal/zed"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "init":
		runInit()

	case "hook":
		runHook()

	case "context":
		runContext()

	case "process":
		runProcess()

	case "index":
		runIndex()

	case "backfill":
		runBackfill()

	case "archive":
		runArchive()

	case "reprocess":
		runReprocess()

	case "check":
		runCheck()

	case "stats":
		runStats()

	case "friction":
		runFriction()

	case "trends":
		runTrends()

	case "inject":
		runInject()

	case "export":
		runExport()

	case "zed":
		runZed()

	case "effectiveness":
		runEffectiveness()

	case "mcp":
		runMcp()

	case "memory":
		runMemory()

	case "vault":
		runVault()

	case "templates":
		runTemplates()

	case "version":
		if wantsHelp(os.Args[2:]) {
			fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdVersion))
			return
		}
		fmt.Printf("vv %s (vibe-vault)\n", help.Version)

	case "help", "--help", "-help", "-h":
		usage()

	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		usage()
		os.Exit(1)
	}
}

func runInit() {
	args := os.Args[2:]
	if wantsHelp(args) {
		fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdInit))
		return
	}

	gitInit := hasFlag(args, "--git")
	args = removeFlag(args, "--git")

	target := "./vibe-vault"
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			fatal("unknown flag: %s\nusage: vv init [path] [--git]", a)
		}
	}
	if len(args) > 0 {
		target = args[0]
	}

	absTarget, err := filepath.Abs(target)
	if err != nil {
		fatal("resolve path: %v", err)
	}

	initAction, initErr := scaffold.Init(absTarget, scaffold.Options{GitInit: gitInit})
	if initErr != nil {
		fatal("init: %v", initErr)
	}

	switch initAction {
	case "adopted":
		fmt.Printf("Adopted existing vault at %s\n", absTarget)
	default:
		fmt.Printf("Created new vault at %s\n", absTarget)
	}

	cfgPath, cfgAction, err := config.WriteDefault(absTarget)
	if err != nil {
		fatal("write config: %v", err)
	}

	switch cfgAction {
	case "created":
		fmt.Printf("Config written to %s\n", cfgPath)
	case "updated":
		fmt.Printf("Config updated: vault_path → %s (%s)\n", config.CompressHome(absTarget), cfgPath)
	case "unchanged":
		fmt.Printf("Config already set to this vault (%s)\n", cfgPath)
	}

	if initAction == "adopted" {
		fmt.Println("\nNext steps:")
		fmt.Println("  1. Run: vv hook install")
		fmt.Println("  2. Run: vv mcp install")
	} else {
		fmt.Println("\nNext steps:")
		fmt.Printf("  1. Open %s in Obsidian\n", absTarget)
		fmt.Println("  2. Install community plugins: Dataview, Templater")
		fmt.Println("  3. Run: vv hook install")
		fmt.Println("  4. Run: vv mcp install")
	}

	fmt.Println("\nTip: enable LLM enrichment for richer session notes — see vv check for status")
}

func runHook() {
	args := os.Args[2:]

	// Route sub-subcommands before falling through to stdin handler
	if len(args) > 0 {
		switch args[0] {
		case "install":
			if wantsHelp(args[1:]) {
				fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdHookInstall))
				return
			}
			if err := hook.Install(); err != nil {
				fatal("%v", err)
			}
			return
		case "uninstall":
			if wantsHelp(args[1:]) {
				fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdHookUninstall))
				return
			}
			if err := hook.Uninstall(); err != nil {
				fatal("%v", err)
			}
			return
		}
	}

	if wantsHelp(args) {
		fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdHook))
		return
	}

	cfg := mustLoadConfig()
	event := flagValue(args, "--event")
	if err := hook.Handle(cfg, event); err != nil {
		fatal("%v", err)
	}
}

func runContext() {
	args := os.Args[2:]

	// Route sub-subcommands
	if len(args) > 0 {
		switch args[0] {
		case "init":
			if wantsHelp(args[1:]) {
				fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdContextInit))
				return
			}
			cfg := mustLoadConfig()
			cwd, err := os.Getwd()
			if err != nil {
				fatal("getwd: %v", err)
			}
			assumeYes := hasFlag(args[1:], "-y") || hasFlag(args[1:], "--yes")
			if ancestor, ok := ancestorMarker(cwd); ok && !assumeYes {
				prompt := fmt.Sprintf("Nested project detected: parent marker at %s.\n  Create marker here anyway?", ancestor)
				if !promptYesNo(prompt) {
					fatal("aborted by user")
				}
			}
			opts := vvcontext.Opts{
				Project: flagValue(args[1:], "--project"),
				Force:   hasFlag(args[1:], "--force"),
			}
			result, err := vvcontext.Init(cfg, cwd, opts)
			if err != nil {
				fatal("%v", err)
			}
			fmt.Printf("Context initialized for project: %s\n\n", result.Project)
			for _, a := range result.Actions {
				printAction(a)
			}
			fmt.Println()
			fmt.Println("Next steps:")
			fmt.Println("  1. Edit .vibe-vault.toml to set your project name, domain, and tags")
			fmt.Println("  2. Commit .vibe-vault.toml to source control to ensure accurate")
			fmt.Println("     project tracking across branches, developers, and platforms")
			fmt.Println("  3. Run 'vv context sync' to propagate shared commands")
			return
		case "migrate":
			if wantsHelp(args[1:]) {
				fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdContextMigrate))
				return
			}
			cfg := mustLoadConfig()
			cwd, err := os.Getwd()
			if err != nil {
				fatal("getwd: %v", err)
			}
			projectFlag := flagValue(args[1:], "--project")
			if projectFlag == "" {
				if _, markerErr := identity.FindMarker(cwd); markerErr != nil {
					fatal("not in a vibe-vault project (no .vibe-vault.toml found) and no --project flag\n  For a first-time migrate, pass --project <name> to specify the target vault project.")
				}
			}
			opts := vvcontext.Opts{
				Project: projectFlag,
				Force:   hasFlag(args[1:], "--force"),
			}
			result, err := vvcontext.Migrate(cfg, cwd, opts)
			if err != nil {
				fatal("%v", err)
			}
			fmt.Printf("Context migrated for project: %s\n\n", result.Project)
			for _, a := range result.Actions {
				printAction(a)
			}
			fmt.Println("\nLocal originals preserved — remove manually after verifying.")
			return
		case "sync":
			if wantsHelp(args[1:]) {
				fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdContextSync))
				return
			}
			cfg := mustLoadConfig()
			cwd, err := os.Getwd()
			if err != nil {
				fatal("getwd: %v", err)
			}
			syncOpts := vvcontext.SyncOpts{
				Project: flagValue(args[1:], "--project"),
				All:     hasFlag(args[1:], "--all"),
				DryRun:  hasFlag(args[1:], "--dry-run"),
				Force:   hasFlag(args[1:], "--force"),
			}
			if !syncOpts.All {
				requireProjectMarker(cwd)
			}
			syncResult, err := vvcontext.Sync(cfg, cwd, syncOpts)
			if err != nil {
				fatal("%v", err)
			}
			for _, psr := range syncResult.Projects {
				if psr.FromVersion == psr.ToVersion && len(psr.Actions) == 0 {
					fmt.Printf("%s: schema v%d (current)\n", psr.Project, psr.ToVersion)
					continue
				}
				if psr.FromVersion != psr.ToVersion {
					fmt.Printf("%s: schema v%d → v%d\n", psr.Project, psr.FromVersion, psr.ToVersion)
				} else {
					fmt.Printf("%s:\n", psr.Project)
				}
				for _, a := range psr.Actions {
					printAction(a)
				}
				if psr.RepoSkipped {
					fmt.Printf("  note: %s\n", psr.RepoNote)
				}
				// Hint for conflicts
				hasConflict := false
				for _, a := range psr.Actions {
					if a.Action == "CONFLICT" {
						hasConflict = true
						break
					}
				}
				if hasConflict {
					fmt.Printf("\n  Conflicts detected. Run: vv context sync --force --project %s\n", psr.Project)
				}
			}
			return
		}
	}

	if wantsHelp(args) {
		fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdContext))
		return
	}

	// No subcommand given — show help
	fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdContext))
	os.Exit(1)
}

func runProcess() {
	if wantsHelp(os.Args[2:]) {
		fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdProcess))
		return
	}

	cfg := mustLoadConfig()
	if len(os.Args) < 3 {
		fatal("usage: vv process <transcript.jsonl>")
	}

	provider, err := llm.NewProvider(cfg.Enrichment)
	if err != nil {
		log.Printf("warning: LLM provider init failed: %v", err)
	}

	path := os.Args[2]
	result, err := session.Capture(session.CaptureOpts{
		TranscriptPath: path,
		Provider:       provider,
	}, cfg)
	if err != nil {
		fatal("process: %v", err)
	}
	if result.Skipped {
		fmt.Printf("skipped: %s\n", result.Reason)
	} else {
		fmt.Printf("created: %s (%s)\n", result.NotePath, result.Title)
	}
}

func runCheck() {
	if wantsHelp(os.Args[2:]) {
		fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdCheck))
		return
	}

	cfg := mustLoadConfig()
	report := check.Run(cfg)

	// Add agentctx schema check for current project (if detectable)
	cwd, err := os.Getwd()
	if err == nil {
		project := session.DetectProject(cwd)
		if project != "_unknown" {
			if result := check.CheckAgentctxSchema(cfg.VaultPath, project, vvcontext.LatestSchemaVersion); result != nil {
				report.Results = append(report.Results, *result)
			}
			if result := check.CheckMemoryLink(cfg.VaultPath, project, cwd); result != nil {
				report.Results = append(report.Results, *result)
			}
			if result := check.CheckCurrentStateInvariants(cfg.VaultPath, project); result != nil {
				report.Results = append(report.Results, *result)
			}
		}
	}

	fmt.Print(report.Format())
	if report.HasFailures() {
		os.Exit(1)
	}
}

func runStats() {
	if wantsHelp(os.Args[2:]) {
		fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdStats))
		return
	}

	cfg := mustLoadConfig()
	project := flagValue(os.Args[2:], "--project")
	source := flagValue(os.Args[2:], "--source")

	idx, err := index.Load(cfg.StateDir())
	if err != nil {
		fatal("load index: %v", err)
	}

	entries := index.FilterBySource(idx.Entries, source)
	summary := stats.Compute(entries, project)
	fmt.Print(stats.Format(summary, project))
}

func runFriction() {
	if wantsHelp(os.Args[2:]) {
		fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdFriction))
		return
	}

	cfg := mustLoadConfig()
	project := flagValue(os.Args[2:], "--project")
	source := flagValue(os.Args[2:], "--source")

	idx, err := index.Load(cfg.StateDir())
	if err != nil {
		fatal("load index: %v", err)
	}

	entries := index.FilterBySource(idx.Entries, source)
	projects := friction.ComputeProjectFriction(entries, project)
	fmt.Print(friction.Format(projects, len(entries), project))
}

func runTrends() {
	if wantsHelp(os.Args[2:]) {
		fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdTrends))
		return
	}

	cfg := mustLoadConfig()
	project := flagValue(os.Args[2:], "--project")
	source := flagValue(os.Args[2:], "--source")

	weeks := 12
	if w := flagValue(os.Args[2:], "--weeks"); w != "" {
		n, err := strconv.Atoi(w)
		if err != nil || n < 1 {
			fatal("--weeks must be a positive integer")
		}
		weeks = n
	}

	idx, err := index.Load(cfg.StateDir())
	if err != nil {
		fatal("load index: %v", err)
	}

	entries := index.FilterBySource(idx.Entries, source)
	result := trends.Compute(entries, project, weeks)
	result.AlertThreshold = cfg.Friction.AlertThreshold
	fmt.Print(trends.Format(result))
}

func runInject() {
	if wantsHelp(os.Args[2:]) {
		fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdInject))
		return
	}

	cfg := mustLoadConfig()

	project := flagValue(os.Args[2:], "--project")
	if project == "" {
		cwd, err := os.Getwd()
		if err == nil {
			project = session.DetectProject(cwd)
		}
	}

	format := flagValue(os.Args[2:], "--format")
	if format == "" {
		format = "md"
	}

	var sections []string
	if s := flagValue(os.Args[2:], "--sections"); s != "" {
		sections = strings.Split(s, ",")
	}

	maxTokens := 2000
	if mt := flagValue(os.Args[2:], "--max-tokens"); mt != "" {
		n, err := strconv.Atoi(mt)
		if err != nil || n < 1 {
			fatal("--max-tokens must be a positive integer")
		}
		maxTokens = n
	}

	idx, err := index.Load(cfg.StateDir())
	if err != nil {
		fatal("load index: %v", err)
	}

	// Check if project has sessions
	hasProject := false
	for _, e := range idx.Entries {
		if e.Project == project {
			hasProject = true
			break
		}
	}
	if !hasProject {
		fmt.Fprintf(os.Stderr, "vv: no sessions found for project %q\n", project)
	}

	trendResult := trends.Compute(idx.Entries, project, 4)

	opts := inject.Opts{
		Project:   project,
		Format:    format,
		Sections:  sections,
		MaxTokens: maxTokens,
	}

	result := inject.Build(idx.Entries, trendResult, opts)
	output, err := inject.Render(result, opts)
	if err != nil {
		fatal("render: %v", err)
	}
	fmt.Print(output)
}

func runEffectiveness() {
	if wantsHelp(os.Args[2:]) {
		fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdEffectiveness))
		return
	}

	cfg := mustLoadConfig()
	project := flagValue(os.Args[2:], "--project")
	format := flagValue(os.Args[2:], "--format")

	idx, err := index.Load(cfg.StateDir())
	if err != nil {
		fatal("load index: %v", err)
	}

	result := effectiveness.Analyze(idx.Entries, project)
	if format == "json" {
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			fatal("marshal: %v", err)
		}
		fmt.Println(string(data))
	} else {
		fmt.Print(effectiveness.Format(result))
	}
}

func runExport() {
	if wantsHelp(os.Args[2:]) {
		fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdExport))
		return
	}

	cfg := mustLoadConfig()

	format := flagValue(os.Args[2:], "--format")
	if format == "" {
		format = "json"
	}
	if format != "json" && format != "csv" {
		fatal("--format must be json or csv")
	}

	project := flagValue(os.Args[2:], "--project")

	idx, err := index.Load(cfg.StateDir())
	if err != nil {
		fatal("load index: %v", err)
	}

	entries := stats.ExportEntries(idx.Entries, project)

	var output string
	switch format {
	case "json":
		output, err = stats.ExportJSON(entries)
	case "csv":
		output, err = stats.ExportCSV(entries)
	}
	if err != nil {
		fatal("export: %v", err)
	}

	fmt.Print(output)
}

func runMemory() {
	args := os.Args[2:]

	if len(args) == 0 {
		fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdMemory))
		os.Exit(1)
	}

	// Only treat --help as "describe the parent" when no subcommand was
	// given; once a subcommand is present the help flag belongs to it.
	sub := args[0]
	if strings.HasPrefix(sub, "-") {
		fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdMemory))
		return
	}
	subArgs := args[1:]

	switch sub {
	case "link":
		if wantsHelp(subArgs) {
			fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdMemoryLink))
			return
		}
		cfg := mustLoadConfig()
		opts := memory.Opts{
			VaultPath:  cfg.VaultPath,
			WorkingDir: flagValue(subArgs, "--working-dir"),
			Force:      hasFlag(subArgs, "--force"),
			DryRun:     hasFlag(subArgs, "--dry-run"),
		}
		res, err := memory.Link(opts)
		if err != nil {
			fatal("memory link: %v", err)
		}
		printMemoryResult("memory link", res, opts.DryRun)

	case "unlink":
		if wantsHelp(subArgs) {
			fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdMemoryUnlink))
			return
		}
		cfg := mustLoadConfig()
		opts := memory.Opts{
			VaultPath:  cfg.VaultPath,
			WorkingDir: flagValue(subArgs, "--working-dir"),
			Force:      hasFlag(subArgs, "--force"),
			DryRun:     hasFlag(subArgs, "--dry-run"),
		}
		res, err := memory.Unlink(opts)
		if err != nil {
			fatal("memory unlink: %v", err)
		}
		printMemoryResult("memory unlink", res, opts.DryRun)

	default:
		fmt.Fprintf(os.Stderr, "unknown memory command: %s\n", sub)
		fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdMemory))
		os.Exit(1)
	}
}

func printMemoryResult(label string, res *memory.Result, dryRun bool) {
	prefix := ""
	if dryRun {
		prefix = "(dry-run) "
	}
	if res.AlreadyLinked {
		fmt.Printf("%s%s: %s: already linked → %s\n", prefix, label, res.Project, res.TargetPath)
		return
	}
	fmt.Printf("%s%s: %s\n", prefix, label, res.Project)
	fmt.Printf("  source: %s\n", res.SourcePath)
	fmt.Printf("  target: %s\n", res.TargetPath)
	for _, a := range res.Actions {
		fmt.Printf("  %-8s %s", a.Kind, a.Path)
		if a.Detail != "" {
			fmt.Printf("  (%s)", a.Detail)
		}
		fmt.Println()
	}
}

func runVault() {
	args := os.Args[2:]

	if len(args) == 0 || wantsHelp(args) {
		fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdVault))
		return
	}

	cfg := mustLoadConfig()

	switch args[0] {
	case "status":
		if wantsHelp(args[1:]) {
			fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdVaultStatus))
			return
		}
		s, err := vaultsync.GetStatus(cfg.VaultPath)
		if err != nil {
			fatal("vault status: %v", err)
		}
		fmt.Printf("branch:  %s\n", s.Branch)
		if s.Clean {
			fmt.Println("status:  clean")
		} else {
			fmt.Println("status:  dirty (uncommitted changes)")
		}
		if s.HasRemote() {
			for _, r := range s.Remotes {
				fmt.Printf("remote:  %s (ahead: %d, behind: %d)\n", r.Name, r.Ahead, r.Behind)
			}
		} else {
			fmt.Println("remote:  (none)")
		}

	case "pull":
		if wantsHelp(args[1:]) {
			fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdVaultPull))
			return
		}
		result, err := vaultsync.Pull(cfg.VaultPath)
		if err != nil {
			fatal("vault pull: %v", err)
		}
		if !result.Updated {
			fmt.Println("vault: already up to date")
		} else {
			fmt.Println("vault: pulled latest changes")
		}
		if result.Regenerate {
			fmt.Println("vault: regenerating auto-generated files...")
			idx, count, err := index.Rebuild(cfg.ProjectsDir(), cfg.StateDir())
			if err != nil {
				fatal("index rebuild: %v", err)
			}
			if err := idx.Save(); err != nil {
				fatal("save index: %v", err)
			}
			if _, err := index.GenerateContext(idx, cfg.VaultPath, contextOpts(cfg)); err != nil {
				log.Printf("warning: generate context: %v", err)
			}
			fmt.Printf("vault: regenerated index (%d sessions)\n", count)
		}
		if len(result.ManualReview) > 0 {
			fmt.Println("vault: files accepted from upstream that may need review:")
			for _, f := range result.ManualReview {
				fmt.Printf("  %s\n", f)
			}
		}

	case "push":
		if wantsHelp(args[1:]) {
			fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdVaultPush))
			return
		}
		msg := flagValue(args[1:], "--message")
		if msg == "" {
			msg = fmt.Sprintf("vault sync %s", time.Now().Format("2006-01-02 15:04"))
		}
		result, err := vaultsync.CommitAndPush(cfg.VaultPath, msg)
		if err != nil {
			fatal("vault push: %v", err)
		}
		if result.CommitSHA == "" {
			fmt.Println("vault: nothing to commit")
		} else if result.AllPushed() {
			fmt.Printf("vault: committed and pushed to %d remote(s) (%s)\n",
				len(result.RemoteResults), result.CommitSHA)
		} else if result.AnyPushed() {
			fmt.Printf("vault: committed (%s), partial push:\n", result.CommitSHA)
			for name, pushErr := range result.RemoteResults {
				if pushErr != nil {
					fmt.Printf("  %s: FAILED (%v)\n", name, pushErr)
				} else {
					fmt.Printf("  %s: ok\n", name)
				}
			}
		} else {
			fmt.Printf("vault: committed (%s) but all pushes failed — resolve manually\n", result.CommitSHA)
		}

	default:
		fmt.Fprintf(os.Stderr, "unknown vault command: %s\n", args[0])
		fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdVault))
		os.Exit(1)
	}
}

func runTemplates() {
	args := os.Args[2:]

	if len(args) > 0 {
		switch args[0] {
		case "list":
			if wantsHelp(args[1:]) {
				fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdTemplatesList))
				return
			}
			cfg := mustLoadConfig()
			reg := templates.New()
			tmplDir := filepath.Join(cfg.VaultPath, "Templates")
			for _, fs := range reg.Compare(tmplDir) {
				fmt.Printf("  %-12s %s\n", fs.Status, fs.Entry.RelPath)
			}
			return

		case "diff":
			if wantsHelp(args[1:]) {
				fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdTemplatesDiff))
				return
			}
			cfg := mustLoadConfig()
			reg := templates.New()
			tmplDir := filepath.Join(cfg.VaultPath, "Templates")
			file := flagValue(args[1:], "--file")
			if file != "" {
				d, err := reg.Diff(tmplDir, file)
				if err != nil {
					fatal("diff: %v", err)
				}
				if d == "" {
					fmt.Println("no differences")
				} else {
					fmt.Print(d)
				}
			} else {
				d := reg.DiffAll(tmplDir)
				if d == "" {
					fmt.Println("all templates match defaults")
				} else {
					fmt.Print(d)
				}
			}
			return

		case "show":
			if wantsHelp(args[1:]) {
				fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdTemplatesShow))
				return
			}
			if len(args) < 2 {
				fatal("usage: vv templates show <name>")
			}
			reg := templates.New()
			content, ok := reg.DefaultContent(args[1])
			if !ok {
				fatal("unknown template: %s", args[1])
			}
			fmt.Print(string(content))
			return

		case "reset":
			if wantsHelp(args[1:]) {
				fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdTemplatesReset))
				return
			}
			cfg := mustLoadConfig()
			reg := templates.New()
			tmplDir := filepath.Join(cfg.VaultPath, "Templates")
			file := flagValue(args[1:], "--file")
			all := hasFlag(args[1:], "--all")
			force := hasFlag(args[1:], "--force")

			if file == "" && !all {
				fatal("specify --file <name> or --all")
			}
			if file != "" && !reg.Has(file) {
				fatal("unknown template: %s", file)
			}

			if !force {
				fmt.Println("dry-run (pass --force to apply):")
				if all {
					for _, fs := range reg.Compare(tmplDir) {
						fmt.Printf("  would reset  %s\n", fs.Entry.RelPath)
					}
				} else {
					fmt.Printf("  would reset  %s\n", file)
				}
				return
			}

			if all {
				actions, err := reg.ResetAll(tmplDir)
				if err != nil {
					fatal("reset: %v", err)
				}
				for _, a := range actions {
					fmt.Printf("  %-8s %s\n", a.Action, a.RelPath)
				}
			} else {
				a, err := reg.Reset(tmplDir, file)
				if err != nil {
					fatal("reset: %v", err)
				}
				fmt.Printf("  %-8s %s\n", a.Action, a.RelPath)
			}
			return
		}
	}

	if wantsHelp(args) {
		fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdTemplates))
		return
	}

	fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdTemplates))
	os.Exit(1)
}

func runZed() {
	args := os.Args[2:]

	if len(args) > 0 {
		switch args[0] {
		case "backfill":
			if wantsHelp(args[1:]) {
				fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdZedBackfill))
				return
			}
			runZedBackfill(args[1:])
			return
		case "list":
			if wantsHelp(args[1:]) {
				fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdZedList))
				return
			}
			runZedList(args[1:])
			return
		case "watch":
			if wantsHelp(args[1:]) {
				fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdZedWatch))
				return
			}
			runZedWatch(args[1:])
			return
		}
	}

	if wantsHelp(args) {
		fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdZed))
		return
	}

	fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdZed))
	os.Exit(1)
}

func runZedBackfill(args []string) {
	cfg := mustLoadConfig()

	dbPath := flagValue(args, "--db")
	if dbPath == "" {
		dbPath = zed.DefaultDBPath()
	}
	if dbPath == "" {
		fatal("cannot determine Zed threads database path")
	}

	projectFilter := flagValue(args, "--project")
	force := hasFlag(args, "--force")

	var since time.Time
	if s := flagValue(args, "--since"); s != "" {
		var err error
		since, err = time.Parse("2006-01-02", s)
		if err != nil {
			fatal("--since must be YYYY-MM-DD format: %v", err)
		}
	}

	// Parse all threads from DB
	fmt.Printf("Reading Zed threads from %s\n", dbPath)
	threads, err := zed.ParseDB(dbPath, zed.ParseOpts{Since: since})
	if err != nil {
		fatal("parse zed db: %v", err)
	}
	fmt.Printf("Found %d threads\n", len(threads))

	processed, skipped, errors := captureZedThreads(threads, dbPath, projectFilter, force, cfg)
	fmt.Printf("\nprocessed: %d, skipped: %d, errors: %d\n", processed, skipped, errors)
}

func runZedList(args []string) {
	dbPath := flagValue(args, "--db")
	if dbPath == "" {
		dbPath = zed.DefaultDBPath()
	}
	if dbPath == "" {
		fatal("cannot determine Zed threads database path")
	}

	var since time.Time
	if s := flagValue(args, "--since"); s != "" {
		var err error
		since, err = time.Parse("2006-01-02", s)
		if err != nil {
			fatal("--since must be YYYY-MM-DD format: %v", err)
		}
	}

	limit := 0
	if l := flagValue(args, "--limit"); l != "" {
		n, err := strconv.Atoi(l)
		if err != nil || n < 1 {
			fatal("--limit must be a positive integer")
		}
		limit = n
	}

	threads, err := zed.ParseDB(dbPath, zed.ParseOpts{Since: since, Limit: limit})
	if err != nil {
		fatal("parse zed db: %v", err)
	}

	if len(threads) == 0 {
		fmt.Println("No threads found.")
		return
	}

	fmt.Printf("%-36s  %-19s  %-6s  %s\n", "ID", "Updated", "Msgs", "Title/Summary")
	fmt.Printf("%-36s  %-19s  %-6s  %s\n", strings.Repeat("-", 36), strings.Repeat("-", 19), strings.Repeat("-", 6), strings.Repeat("-", 40))
	for _, t := range threads {
		title := t.Title
		if title == "" {
			title = t.Summary
		}
		if len(title) > 60 {
			title = title[:57] + "..."
		}
		updated := t.UpdatedAt.Format("2006-01-02 15:04:05")
		fmt.Printf("%-36s  %-19s  %-6d  %s\n", t.ID, updated, len(t.Messages), title)
	}
}

func runZedWatch(args []string) {
	cfg := mustLoadConfig()

	dbPath := flagValue(args, "--db")
	if dbPath == "" && cfg.Zed.DBPath != "" {
		dbPath = cfg.Zed.DBPath
	}
	if dbPath == "" {
		dbPath = zed.DefaultDBPath()
	}
	if dbPath == "" {
		fatal("cannot determine Zed threads database path")
	}

	projectFilter := flagValue(args, "--project")

	debounce := time.Duration(cfg.Zed.DebounceMinutes) * time.Minute
	if d := flagValue(args, "--debounce"); d != "" {
		parsed, err := time.ParseDuration(d)
		if err != nil {
			fatal("--debounce: %v", err)
		}
		debounce = parsed
	}

	fmt.Printf("Watching %s (debounce %s)\n", dbPath, debounce)
	if projectFilter != "" {
		fmt.Printf("Filtering for project: %s\n", projectFilter)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	// Mutex to prevent concurrent callback invocations
	var mu sync.Mutex

	err := zed.Watch(ctx, zed.WatcherConfig{
		DBPath:   dbPath,
		Debounce: debounce,
		Logger:   log.Default(),
	}, func() {
		mu.Lock()
		defer mu.Unlock()

		window := debounce + time.Minute
		since := time.Now().Add(-window)

		threads, err := zed.ParseDB(dbPath, zed.ParseOpts{Since: since})
		if err != nil {
			log.Printf("error reading threads: %v", err)
			return
		}

		if len(threads) == 0 {
			return
		}

		processed, skipped, errors := captureZedThreads(threads, dbPath, projectFilter, false, cfg)
		if processed > 0 || errors > 0 {
			fmt.Printf("[%s] captured: %d, skipped: %d, errors: %d\n",
				time.Now().Format("15:04:05"), processed, skipped, errors)
		}
	})

	if err != nil && err != context.Canceled {
		fatal("watcher: %v", err)
	}
}

// captureZedThreads processes a batch of Zed threads through the capture pipeline.
// Shared between runZedBackfill and runZedWatch.
// Thin wrapper around zed.BatchCapture that prints to stdout.
func captureZedThreads(threads []zed.Thread, dbPath, projectFilter string, force bool, cfg config.Config) (processed, skipped, errors int) {
	// Use a logger that prints to stdout for interactive CLI output
	logger := log.New(os.Stdout, "", 0)
	result := zed.BatchCapture(zed.BatchCaptureOpts{
		Threads:       threads,
		DBPath:        dbPath,
		ProjectFilter: projectFilter,
		Force:         force,
		Cfg:           cfg,
		Logger:        logger,
	})
	return result.Processed, result.Skipped, result.Errors
}

// registerMCPTools registers all production tools and prompts on the server.
func registerMCPTools(srv *mcp.Server, cfg config.Config) {
	srv.RegisterTool(mcp.NewGetProjectContextTool(cfg))
	srv.RegisterTool(mcp.NewListProjectsTool(cfg))
	srv.RegisterTool(mcp.NewSearchSessionsTool(cfg))
	srv.RegisterTool(mcp.NewGetKnowledgeTool(cfg))
	srv.RegisterTool(mcp.NewGetSessionDetailTool(cfg))
	srv.RegisterTool(mcp.NewGetFrictionTrendsTool(cfg))
	srv.RegisterTool(mcp.NewGetEffectivenessTool(cfg))
	srv.RegisterTool(mcp.NewCaptureSessionTool(cfg))
	srv.RegisterTool(mcp.NewGetWorkflowTool(cfg))
	srv.RegisterTool(mcp.NewGetResumeTool(cfg))
	srv.RegisterTool(mcp.NewListTasksTool(cfg))
	srv.RegisterTool(mcp.NewGetTaskTool(cfg))
	srv.RegisterTool(mcp.NewUpdateResumeTool(cfg))
	srv.RegisterTool(mcp.NewAppendIterationTool(cfg))
	srv.RegisterTool(mcp.NewManageTaskTool(cfg))
	srv.RegisterTool(mcp.NewRefreshIndexTool(cfg))
	srv.RegisterTool(mcp.NewBootstrapContextTool(cfg))
	srv.RegisterTool(mcp.NewListLearningsTool(cfg))
	srv.RegisterTool(mcp.NewGetLearningTool(cfg))
	srv.RegisterTool(mcp.NewGetIterationsTool(cfg))
	srv.RegisterPrompt(mcp.NewSessionGuidelinesPrompt())
}

const mcpInstructions = `Call vv_bootstrap_context at session start for full project context. Use vv_capture_session at the end of each work unit.`

func runMcp() {
	args := os.Args[2:]

	// Parse --debug before subcommand dispatch.
	debug := hasFlag(args, "--debug")
	if debug {
		// Filter --debug from args so subcommands don't see it.
		filtered := make([]string, 0, len(args))
		for _, a := range args {
			if a != "--debug" {
				filtered = append(filtered, a)
			}
		}
		args = filtered
	}

	if len(args) > 0 {
		switch args[0] {
		case "install":
			if wantsHelp(args[1:]) {
				fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdMcpInstall))
				return
			}
			claudeOnly := hasFlag(args[1:], "--claude-only")
			zedOnly := hasFlag(args[1:], "--zed-only")
			claudePlugin := hasFlag(args[1:], "--claude-plugin")
			if claudePlugin && (claudeOnly || zedOnly) {
				fatal("--claude-plugin cannot be combined with --claude-only or --zed-only")
			}
			if claudeOnly && zedOnly {
				fatal("--claude-only and --zed-only are mutually exclusive")
			}
			if hasFlag(args[1:], "--zed") {
				fmt.Fprintf(os.Stderr, "note: --zed is deprecated; default now installs all detected editors\n")
				fmt.Fprintf(os.Stderr, "      use --zed-only to install only Zed\n\n")
				zedOnly = true
			}
			if claudePlugin {
				if err := hook.InstallClaudePlugin(); err != nil {
					fatal("%v", err)
				}
				return
			}
			if err := hook.InstallMCPAll(claudeOnly, zedOnly); err != nil {
				fatal("%v", err)
			}
			return
		case "uninstall":
			if wantsHelp(args[1:]) {
				fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdMcpUninstall))
				return
			}
			claudeOnly := hasFlag(args[1:], "--claude-only")
			zedOnly := hasFlag(args[1:], "--zed-only")
			claudePlugin := hasFlag(args[1:], "--claude-plugin")
			if claudePlugin && (claudeOnly || zedOnly) {
				fatal("--claude-plugin cannot be combined with --claude-only or --zed-only")
			}
			if claudeOnly && zedOnly {
				fatal("--claude-only and --zed-only are mutually exclusive")
			}
			if hasFlag(args[1:], "--zed") {
				fmt.Fprintf(os.Stderr, "note: --zed is deprecated; default now uninstalls from all detected editors\n")
				fmt.Fprintf(os.Stderr, "      use --zed-only to uninstall only Zed\n\n")
				zedOnly = true
			}
			if claudePlugin {
				if err := hook.UninstallClaudePlugin(); err != nil {
					fatal("%v", err)
				}
				return
			}
			if err := hook.UninstallMCPAll(claudeOnly, zedOnly); err != nil {
				fatal("%v", err)
			}
			return
		case "check":
			if wantsHelp(args[1:]) {
				fmt.Fprintf(os.Stderr, "Usage: vv mcp check\n\nRuns MCP protocol compliance checks against the production server.\n")
				return
			}
			cfg := mustLoadConfig()
			logger := log.New(os.Stderr, "", log.LstdFlags)
			srv := mcp.NewServer(mcp.ServerInfo{Name: "vibe-vault", Version: help.Version}, logger)
			registerMCPTools(srv, cfg)
			srv.SetInstructions(mcpInstructions)
			results := mcp.RunChecks(srv)
			failed := false
			for _, r := range results {
				status := "PASS"
				if !r.Pass {
					status = "FAIL"
					failed = true
				}
				fmt.Printf("[%s] %s\n", status, r.Name)
				if r.Detail != "" {
					fmt.Printf("       %s\n", r.Detail)
				}
			}
			if failed {
				os.Exit(1)
			}
			return
		}
	}
	if wantsHelp(args) {
		fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdMcp))
		return
	}
	cfg := mustLoadConfig()
	logger := log.New(os.Stderr, "", log.LstdFlags)
	srv := mcp.NewServer(mcp.ServerInfo{Name: "vibe-vault", Version: help.Version}, logger)
	registerMCPTools(srv, cfg)
	srv.SetInstructions(mcpInstructions)
	if debug {
		srv.SetDebug(true)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	if cfg.Zed.AutoCapture {
		dbPath := cfg.Zed.DBPath
		if dbPath == "" {
			dbPath = zed.DefaultDBPath()
		}
		debounce := time.Duration(cfg.Zed.DebounceMinutes) * time.Minute
		if debounce == 0 {
			debounce = 5 * time.Minute
		}
		errCh := mcp.StartAutoCapture(ctx, mcp.AutoCaptureConfig{
			DBPath:   dbPath,
			Debounce: debounce,
			Logger:   logger,
			Cfg:      cfg,
		})
		defer func() {
			cancel() // stop watcher before draining
			if err := <-errCh; err != nil && !errors.Is(err, context.Canceled) {
				logger.Printf("auto-capture watcher error: %v", err)
			}
		}()
	}

	if err := srv.Serve(ctx, os.Stdin, os.Stdout); err != nil {
		logger.Printf("mcp server error: %v", err)
		os.Exit(1)
	}
}

func runIndex() {
	if wantsHelp(os.Args[2:]) {
		fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdIndex))
		return
	}

	cfg := mustLoadConfig()

	idx, count, err := index.Rebuild(cfg.ProjectsDir(), cfg.StateDir())
	if err != nil {
		fatal("index: %v", err)
	}

	if err := idx.Save(); err != nil {
		fatal("save index: %v", err)
	}

	fmt.Printf("indexed %d sessions\n", count)

	if _, err := index.GenerateContext(idx, cfg.VaultPath, contextOpts(cfg)); err != nil {
		log.Printf("warning: generate context: %v", err)
	} else {
		for _, project := range idx.Projects() {
			fmt.Printf("  context: %s\n", filepath.Join("Projects", project, "history.md"))
		}
	}
}

func defaultTranscriptDir() string {
	home, err := meta.HomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "projects")
}

func runBackfill() {
	if wantsHelp(os.Args[2:]) {
		fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdBackfill))
		return
	}

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
	if wantsHelp(os.Args[2:]) {
		fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdArchive))
		return
	}

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
	if wantsHelp(os.Args[2:]) {
		fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdReprocess))
		return
	}

	cfg := mustLoadConfig()

	// --backfill-context: populate ContextAvailable on entries, then exit
	if hasFlag(os.Args[2:], "--backfill-context") {
		overwrite := hasFlag(os.Args[2:], "--force")
		indexPath := filepath.Join(cfg.StateDir(), "session-index.json")
		fl, lockErr := index.Lock(indexPath)
		if lockErr != nil {
			fatal("acquire index lock: %v", lockErr)
		}
		defer func() { _ = fl.Unlock() }()

		idx, err := index.Load(cfg.StateDir())
		if err != nil {
			fatal("load index: %v", err)
		}
		result := idx.BackfillContext(overwrite)
		if err := idx.Save(); err != nil {
			fatal("save index: %v", err)
		}
		fmt.Printf("backfill-context: updated %d, skipped %d\n", result.Updated, result.Skipped)
		return
	}

	archiveDir := filepath.Join(cfg.StateDir(), "archive")
	projectFilter := flagValue(os.Args[2:], "--project")
	sourceFilter := flagValue(os.Args[2:], "--source")
	dryRun := hasFlag(os.Args[2:], "--dry-run")

	// Create LLM provider for enrichment.
	var provider llm.Provider
	if !dryRun {
		var providerErr error
		provider, providerErr = llm.NewProvider(cfg.Enrichment)
		if providerErr != nil {
			log.Printf("warning: LLM provider init failed: %v", providerErr)
		}
	}

	// Report mode.
	if dryRun {
		fmt.Println("Dry run — no files will be written")
	} else if providerName, model, reason := llm.Available(cfg.Enrichment); reason == "" {
		fmt.Printf("Reprocessing with LLM enrichment (%s/%s)\n", providerName, model)
	} else {
		fmt.Println("Reprocessing with heuristic extraction only (no LLM configured)")
	}

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
		// Source filter: "zed" matches Source=="zed" or zed: transcript prefix;
		// "claude-code" matches empty Source field.
		if sourceFilter != "" {
			isZed := entry.Source == "zed" || strings.HasPrefix(entry.TranscriptPath, "zed:")
			if sourceFilter == "zed" && !isZed {
				continue
			}
			if sourceFilter != "zed" && isZed {
				continue
			}
		}

		// Zed entries: re-capture from threads.db
		if entry.Source == "zed" || strings.HasPrefix(entry.TranscriptPath, "zed:") {
			if dryRun {
				result, err := dryRunZedEntry(entry, cfg)
				if err != nil {
					log.Printf("error (dry-run) zed entry %s: %v", entry.SessionID, err)
					errors++
					continue
				}
				if result.newProject != entry.Project {
					fmt.Printf("  %s → %s (was %s)\n", entry.SessionID[:min(12, len(entry.SessionID))], result.newProject, entry.Project)
					processed++
					affectedProjects[result.newProject] = true
				} else {
					skipped++
				}
				continue
			}

			result, err := reprocessZedEntry(entry, cfg)
			if err != nil {
				log.Printf("error reprocessing zed entry %s: %v", entry.SessionID, err)
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
			continue
		}

		if dryRun {
			skipped++ // dry-run for Claude Code entries not implemented
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
			Provider:       provider,
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
	if !dryRun && len(affectedProjects) > 0 {
		idx, _ = index.Load(cfg.StateDir())
		if _, err := index.GenerateContext(idx, cfg.VaultPath, contextOpts(cfg)); err != nil {
			log.Printf("warning: generate context: %v", err)
		} else {
			for project := range affectedProjects {
				fmt.Printf("  context: %s\n", filepath.Join("Projects", project, "history.md"))
			}
		}
	}

	label := "reprocessed"
	if dryRun {
		label = "would reprocess"
	}
	fmt.Printf("\n%s: %d, skipped: %d, errors: %d\n", label, processed, skipped, errors)
}

// dryRunResult holds the detection result for a dry-run reprocess.
type dryRunResult struct {
	newProject string
}

// dryRunZedEntry runs detection on a Zed entry without writing anything.
func dryRunZedEntry(entry index.SessionEntry, cfg config.Config) (*dryRunResult, error) {
	dbPath, threadID, ok := parseZedTranscriptPath(entry.TranscriptPath)
	if !ok {
		return nil, fmt.Errorf("invalid zed transcript path: %s", entry.TranscriptPath)
	}

	thread, err := zed.QueryThread(dbPath, threadID)
	if err != nil {
		return nil, fmt.Errorf("query thread: %w", err)
	}

	info := zed.DetectProject(thread, cfg)
	return &dryRunResult{newProject: info.Project}, nil
}

// parseZedTranscriptPath splits "zed:/path/to/db#thread-id" into db path and thread ID.
func parseZedTranscriptPath(tp string) (dbPath, threadID string, ok bool) {
	tp = strings.TrimPrefix(tp, "zed:")
	idx := strings.LastIndex(tp, "#")
	if idx < 0 || idx == len(tp)-1 {
		return "", "", false
	}
	return tp[:idx], tp[idx+1:], true
}

func reprocessZedEntry(entry index.SessionEntry, cfg config.Config) (*session.CaptureResult, error) {
	dbPath, threadID, ok := parseZedTranscriptPath(entry.TranscriptPath)
	if !ok {
		return nil, fmt.Errorf("invalid zed transcript path: %s", entry.TranscriptPath)
	}

	thread, err := zed.QueryThread(dbPath, threadID)
	if err != nil {
		return nil, fmt.Errorf("query thread: %w", err)
	}

	info := zed.DetectProject(thread, cfg)
	t, err := zed.Convert(thread)
	if err != nil {
		return nil, fmt.Errorf("convert thread: %w", err)
	}
	if t == nil {
		return &session.CaptureResult{Skipped: true, Reason: "empty thread"}, nil
	}

	narr := zed.ExtractNarrative(thread)
	dialogue := zed.ExtractDialogue(thread)

	opts := session.CaptureOpts{
		TranscriptPath: entry.TranscriptPath,
		Source:         "zed",
		Force:          true,
		SkipEnrichment: true,
	}

	return session.CaptureFromParsed(t, info, narr, dialogue, opts, cfg)
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
	fmt.Fprint(os.Stderr, help.FormatUsage(help.TopLevel, help.Subcommands))
}

func mustLoadConfig() config.Config {
	cfg, err := config.Load()
	if err != nil {
		fatal("load config: %v", err)
	}
	return cfg
}

func wantsHelp(args []string) bool {
	for _, a := range args {
		if a == "--help" || a == "-help" || a == "-h" {
			return true
		}
	}
	return false
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

func contextOpts(cfg config.Config) index.ContextOptions {
	return index.ContextOptions{
		AlertThreshold:       cfg.Friction.AlertThreshold,
		TimelineRecentDays:   cfg.History.TimelineRecentDays,
		TimelineWindowDays:   cfg.History.TimelineWindowDays,
		DecisionStaleDays:    cfg.History.DecisionStaleDays,
		KeyFilesRecencyBoost: cfg.History.KeyFilesRecencyBoost,
	}
}

func printAction(a vvcontext.FileAction) {
	if a.Location != "" {
		fmt.Printf("  %-8s [%s] %s\n", a.Action, a.Location, a.Path)
	} else {
		fmt.Printf("  %-8s %s\n", a.Action, a.Path)
	}
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "vv: "+format+"\n", args...)
	os.Exit(1)
}

// requireProjectMarker fatal-exits unless .vibe-vault.toml is found in cwd or
// any ancestor. Used by `vv context {sync,migrate}` — the guard is skipped by
// `vv context sync --all`, which operates on vault state independent of cwd.
func requireProjectMarker(cwd string) {
	if _, err := identity.FindMarker(cwd); err != nil {
		fatal("not in a vibe-vault project (no .vibe-vault.toml found)\n  Run `vv init` in your project root first, or cd into an existing vibe-vault project.")
	}
}

// ancestorMarker reports whether any strict ancestor of cwd contains a
// .vibe-vault.toml. Used by `vv context init` to warn about nested projects.
func ancestorMarker(cwd string) (string, bool) {
	parent := filepath.Dir(cwd)
	if parent == cwd {
		return "", false
	}
	dir, err := identity.FindMarker(parent)
	if err != nil {
		return "", false
	}
	return dir, true
}

// promptYesNo writes the question to stderr and reads a single line from
// stdin. Returns true only on an affirmative response; any other input,
// EOF, or read error is treated as "no".
func promptYesNo(question string) bool {
	fmt.Fprint(os.Stderr, question+" [y/N] ")
	var buf [256]byte
	n, err := os.Stdin.Read(buf[:])
	if err != nil || n == 0 {
		return false
	}
	r := strings.TrimSpace(strings.ToLower(string(buf[:n])))
	return r == "y" || r == "yes"
}
