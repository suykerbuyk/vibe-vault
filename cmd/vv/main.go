// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/johns/vibe-vault/internal/archive"
	"github.com/johns/vibe-vault/internal/check"
	"github.com/johns/vibe-vault/internal/config"
	vvcontext "github.com/johns/vibe-vault/internal/context"
	"github.com/johns/vibe-vault/internal/discover"
	"github.com/johns/vibe-vault/internal/friction"
	"github.com/johns/vibe-vault/internal/help"
	"github.com/johns/vibe-vault/internal/hook"
	"github.com/johns/vibe-vault/internal/index"
	"github.com/johns/vibe-vault/internal/inject"
	"github.com/johns/vibe-vault/internal/llm"
	"github.com/johns/vibe-vault/internal/scaffold"
	"github.com/johns/vibe-vault/internal/session"
	"github.com/johns/vibe-vault/internal/stats"
	"github.com/johns/vibe-vault/internal/templates"
	"github.com/johns/vibe-vault/internal/trends"
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

	fmt.Printf("Creating vault at %s\n", absTarget)

	if err := scaffold.Init(absTarget, scaffold.Options{GitInit: gitInit}); err != nil {
		fatal("init: %v", err)
	}

	cfgPath, action, err := config.WriteDefault(absTarget)
	if err != nil {
		fatal("write config: %v", err)
	}

	fmt.Println("\nDone! Next steps:")
	fmt.Printf("  1. Open %s in Obsidian\n", absTarget)
	fmt.Println("  2. Install community plugins: Dataview, Templater")
	fmt.Println("  3. Run: vv hook install")

	switch action {
	case "created":
		fmt.Printf("\nConfig written to %s\n", cfgPath)
	case "updated":
		fmt.Printf("\nConfig updated: vault_path → %s (%s)\n", config.CompressHome(absTarget), cfgPath)
	case "unchanged":
		fmt.Printf("\nConfig already set to this vault (%s)\n", cfgPath)
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
				fmt.Printf("  %-8s %s\n", a.Action, a.Path)
			}
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
			opts := vvcontext.Opts{
				Project: flagValue(args[1:], "--project"),
				Force:   hasFlag(args[1:], "--force"),
			}
			result, err := vvcontext.Migrate(cfg, cwd, opts)
			if err != nil {
				fatal("%v", err)
			}
			fmt.Printf("Context migrated for project: %s\n\n", result.Project)
			for _, a := range result.Actions {
				fmt.Printf("  %-8s %s\n", a.Action, a.Path)
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
					fmt.Printf("  %-8s %s\n", a.Action, a.Path)
				}
				if psr.RepoSkipped {
					fmt.Printf("  note: %s\n", psr.RepoNote)
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

	idx, err := index.Load(cfg.StateDir())
	if err != nil {
		fatal("load index: %v", err)
	}

	summary := stats.Compute(idx.Entries, project)
	fmt.Print(stats.Format(summary, project))
}

func runFriction() {
	if wantsHelp(os.Args[2:]) {
		fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdFriction))
		return
	}

	cfg := mustLoadConfig()
	project := flagValue(os.Args[2:], "--project")

	idx, err := index.Load(cfg.StateDir())
	if err != nil {
		fatal("load index: %v", err)
	}

	projects := friction.ComputeProjectFriction(idx.Entries, project)
	fmt.Print(friction.Format(projects, len(idx.Entries), project))
}

func runTrends() {
	if wantsHelp(os.Args[2:]) {
		fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdTrends))
		return
	}

	cfg := mustLoadConfig()
	project := flagValue(os.Args[2:], "--project")

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

	result := trends.Compute(idx.Entries, project, weeks)
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

	if _, err := index.GenerateContext(idx, cfg.VaultPath, cfg.Friction.AlertThreshold); err != nil {
		log.Printf("warning: generate context: %v", err)
	} else {
		for _, project := range idx.Projects() {
			fmt.Printf("  context: %s\n", filepath.Join("Projects", project, "history.md"))
		}
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
	archiveDir := filepath.Join(cfg.StateDir(), "archive")
	projectFilter := flagValue(os.Args[2:], "--project")

	// Create LLM provider for enrichment.
	provider, providerErr := llm.NewProvider(cfg.Enrichment)
	if providerErr != nil {
		log.Printf("warning: LLM provider init failed: %v", providerErr)
	}

	// Report enrichment mode.
	if providerName, model, reason := llm.Available(cfg.Enrichment); reason == "" {
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
	if len(affectedProjects) > 0 {
		idx, _ = index.Load(cfg.StateDir())
		if _, err := index.GenerateContext(idx, cfg.VaultPath, cfg.Friction.AlertThreshold); err != nil {
			log.Printf("warning: generate context: %v", err)
		} else {
			for project := range affectedProjects {
				fmt.Printf("  context: %s\n", filepath.Join("Projects", project, "history.md"))
			}
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

func fatal(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "vv: "+format+"\n", args...)
	os.Exit(1)
}
