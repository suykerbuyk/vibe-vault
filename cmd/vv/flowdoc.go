// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/suykerbuyk/vibe-vault/internal/config"
	"github.com/suykerbuyk/vibe-vault/internal/flowdoc"
	"github.com/suykerbuyk/vibe-vault/internal/llm"
	"github.com/suykerbuyk/vibe-vault/internal/session"
)

// defaultFlowdocOutputTokens is the LLM response MaxTokens for a
// generation run when --max-output-tokens is not given. The
// flowdoc-gen-source-ingestion task (finding M3) flags the safe ceiling
// as model-dependent and to-be-discovered during the Phase-3
// measurement; the flag exists so that discovery does not need a
// rebuild.
const defaultFlowdocOutputTokens = 16384

// defaultAgenticModel is the Anthropic model used by the agentic
// strategy when --model is not given. Per the Phase-4 plan: haiku is
// the cheapest tool-use-capable Anthropic model; sonnet/opus are
// available via --model. (Grok / OpenAI cannot do agentic today —
// flowdoc-gen-source-ingestion anti-scope.)
const defaultAgenticModel = "claude-haiku-4-5"

// defaultAgenticMaxIterations bounds the agentic tool-use loop. The
// pre-retirement default was 10 (sized for wrap-dispatch); repo
// exploration is heavier — the iter-236 spike that motivated this work
// used Read/Glob/Grep tens of times. 30 leaves headroom over typical
// runs without letting a runaway loop burn unbounded money.
const defaultAgenticMaxIterations = 30

// defaultFlowdocModel is the hard fallback when neither --model nor
// the configured enrichment.model is set. Per the flowdoc-command v2
// plan, grok-4-fast is the cheap, fast, large-context default well
// suited to ingesting a whole repo tree in one shot.
const defaultFlowdocModel = "grok-4-fast"

// newProviderForFlowdoc constructs the LLM provider for `vv flowdoc gen`.
// It is a package-level variable so tests can swap in a stub provider
// without touching real credentials or the network. The production
// implementation mirrors the synthesis.go pattern: build via
// llm.NewProvider, then override Model on the request rather than
// re-constructing the provider per model.
var newProviderForFlowdoc = func(cfg config.Config) (llm.Provider, error) {
	return llm.NewProvider(cfg.Enrichment, cfg.Providers)
}

// newAgenticProviderForFlowdoc constructs the agentic (Anthropic
// multi-turn tool-use) provider. Mirrors newProviderForFlowdoc so tests
// can swap in a fake AgenticProvider without real credentials.
var newAgenticProviderForFlowdoc = func(model string, cfg config.Config) (llm.AgenticProvider, error) {
	return llm.NewAgenticProvider(model, cfg.Providers)
}

// runFlowdoc dispatches `vv flowdoc <verb>`. Returns the process exit
// code; main.go's switch wires this through directly.
func runFlowdoc(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: vv flowdoc <gen|verify> [...]")
		return 2
	}
	switch args[0] {
	case "gen":
		return runFlowdocGen(args[1:])
	case "verify":
		return runFlowdocVerify(args[1:])
	case "--help", "-help", "-h", "help":
		fmt.Fprintln(os.Stderr, "usage: vv flowdoc <gen|verify> [...]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "subcommands:")
		fmt.Fprintln(os.Stderr, "  gen      Generate doc/flows.json + doc/FLOWS.html via LLM")
		fmt.Fprintln(os.Stderr, "  verify   Check doc/flows.json refs against the source tree (zero-LLM)")
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown flowdoc verb: %s\n", args[0])
		return 2
	}
}

// flowdocGenOpts holds parsed flag values for `vv flowdoc gen`.
// Extracted as a struct so tests can assert parse outcomes without
// driving the full LLM happy path.
type flowdocGenOpts struct {
	project         string
	model           string
	open            bool
	dryRun          bool   // --dry-run / --show-context: inspect the context, no LLM call
	maxContextBytes int    // --max-context-bytes: file-content budget; 0 → DefaultContextBudgetBytes
	maxOutputTokens int    // --max-output-tokens: LLM response cap; 0 → defaultFlowdocOutputTokens
	strategy        string // --strategy: auto|agentic|single-shot; "" → auto
	maxIterations   int    // --max-iterations: agentic tool-use cap; 0 → defaultAgenticMaxIterations
}

// parseFlowdocGenArgs parses the flag set for `vv flowdoc gen`. Unknown
// flags or values are reported on stderr and surface as a non-nil error
// so the caller can return a non-zero exit code without exiting from
// inside the parser (tests need a clean return).
func parseFlowdocGenArgs(args []string) (flowdocGenOpts, error) {
	var opts flowdocGenOpts
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--help", a == "-h", a == "-help":
			return opts, fmt.Errorf("help requested")
		case a == "--open":
			opts.open = true
		case a == "--dry-run", a == "--show-context":
			opts.dryRun = true
		case a == "--max-context-bytes":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--max-context-bytes requires a value")
			}
			n, err := strconv.Atoi(args[i+1])
			if err != nil {
				return opts, fmt.Errorf("--max-context-bytes: %v", err)
			}
			opts.maxContextBytes = n
			i++
		case strings.HasPrefix(a, "--max-context-bytes="):
			n, err := strconv.Atoi(strings.TrimPrefix(a, "--max-context-bytes="))
			if err != nil {
				return opts, fmt.Errorf("--max-context-bytes: %v", err)
			}
			opts.maxContextBytes = n
		case a == "--max-output-tokens":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--max-output-tokens requires a value")
			}
			n, err := strconv.Atoi(args[i+1])
			if err != nil {
				return opts, fmt.Errorf("--max-output-tokens: %v", err)
			}
			opts.maxOutputTokens = n
			i++
		case strings.HasPrefix(a, "--max-output-tokens="):
			n, err := strconv.Atoi(strings.TrimPrefix(a, "--max-output-tokens="))
			if err != nil {
				return opts, fmt.Errorf("--max-output-tokens: %v", err)
			}
			opts.maxOutputTokens = n
		case a == "--strategy":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--strategy requires a value (auto|agentic|single-shot)")
			}
			opts.strategy = args[i+1]
			i++
		case strings.HasPrefix(a, "--strategy="):
			opts.strategy = strings.TrimPrefix(a, "--strategy=")
		case a == "--max-iterations":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--max-iterations requires a value")
			}
			n, err := strconv.Atoi(args[i+1])
			if err != nil {
				return opts, fmt.Errorf("--max-iterations: %v", err)
			}
			opts.maxIterations = n
			i++
		case strings.HasPrefix(a, "--max-iterations="):
			n, err := strconv.Atoi(strings.TrimPrefix(a, "--max-iterations="))
			if err != nil {
				return opts, fmt.Errorf("--max-iterations: %v", err)
			}
			opts.maxIterations = n
		case a == "--project":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--project requires a value")
			}
			opts.project = args[i+1]
			i++
		case strings.HasPrefix(a, "--project="):
			opts.project = strings.TrimPrefix(a, "--project=")
		case a == "--model":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--model requires a value")
			}
			opts.model = args[i+1]
			i++
		case strings.HasPrefix(a, "--model="):
			opts.model = strings.TrimPrefix(a, "--model=")
		default:
			return opts, fmt.Errorf("unknown flag: %s", a)
		}
	}
	return opts, nil
}

// runFlowdocGen handles `vv flowdoc gen`. Returns the process exit code.
//
// Behavior:
//   - Resolves project name from --project, then session.DetectProject,
//     then filepath.Base(cwd).
//   - Resolves model from --model, then config Enrichment.Model, then
//     defaultFlowdocModel.
//   - Calls the configured LLM provider with flowdoc.Brief as system
//     prompt; receives flows.json as content.
//   - Strips a leading ```json / trailing ``` markdown fence if present.
//   - Unmarshals into flowdoc.FlowDoc and validates.
//   - Writes <project-root>/doc/flows.json (indented) and FLOWS.html.
//   - With --open, fires xdg-open (linux) / open (macOS) on the HTML.
func runFlowdocGen(args []string) int {
	opts, err := parseFlowdocGenArgs(args)
	if err != nil {
		if err.Error() == "help requested" {
			fmt.Fprintln(os.Stderr, "usage: vv flowdoc gen [--project <name>] [--model <name>] [--strategy auto|agentic|single-shot] [--max-iterations <n>] [--max-context-bytes <n>] [--max-output-tokens <n>] [--dry-run] [--open]")
			return 0
		}
		fmt.Fprintf(os.Stderr, "flowdoc gen: %v\n", err)
		fmt.Fprintln(os.Stderr, "usage: vv flowdoc gen [--project <name>] [--model <name>] [--strategy auto|agentic|single-shot] [--max-iterations <n>] [--max-context-bytes <n>] [--max-output-tokens <n>] [--dry-run] [--open]")
		return 2
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "flowdoc gen: getwd: %v\n", err)
		return 1
	}

	// Project name resolution: explicit flag wins; otherwise the same
	// session.DetectProject helper the capture pipeline uses; final
	// fallback to filepath.Base so we always have *some* label.
	project := opts.project
	if project == "" {
		project = session.DetectProject(cwd)
		if project == "" || project == "_unknown" {
			project = filepath.Base(cwd)
		}
	}

	// Project root for the output paths: the first ancestor of cwd with
	// a .git entry (covers both regular checkouts and git worktrees).
	// Fall back to cwd so `vv flowdoc gen` still works in a non-git
	// directory, just emitting doc/ alongside whatever the operator is in.
	projectRoot := session.DetectProjectRoot(cwd)
	if projectRoot == "" {
		projectRoot = cwd
	}

	// Repo introspection: enumerate the source tree and pick the key
	// files whose contents will be inlined into the prompt. This is the
	// input-gathering step the shipped single-shot command never had
	// (flowdoc-gen-source-ingestion); without it the model receives no
	// repo bytes and hallucinates a plausible project.
	view, err := flowdoc.WalkRepo(projectRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "flowdoc gen: walk repo: %v\n", err)
		return 1
	}
	keyFiles := flowdoc.SelectKeyFiles(view)

	maxContextBytes := opts.maxContextBytes
	if maxContextBytes == 0 {
		maxContextBytes = flowdoc.DefaultContextBudgetBytes
	}

	// --dry-run / --show-context: print the would-be prompt without an
	// LLM call. Auto/blank strategy defaults to single-shot so dry-run
	// stays config-free; explicit --strategy agentic shows the tool
	// catalogue + the agentic initial prompt instead.
	if opts.dryRun {
		s := opts.strategy
		if s == "" || s == "auto" {
			s = "single-shot"
		}
		switch s {
		case "agentic":
			return runFlowdocDryRunAgentic(project, projectRoot, view, opts)
		case "single-shot":
			return runFlowdocDryRunSingleShot(project, projectRoot, view, keyFiles, maxContextBytes)
		default:
			fmt.Fprintf(os.Stderr, "flowdoc gen: unknown --strategy %q (want auto|agentic|single-shot)\n", s)
			return 2
		}
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "flowdoc gen: load config: %v\n", err)
		return 1
	}

	strategy, err := chooseStrategy(opts, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "flowdoc gen: %v\n", err)
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	var rawJSON string
	switch strategy {
	case "agentic":
		rawJSON, err = runFlowdocAgenticLLM(ctx, project, view, opts, cfg)
	case "single-shot":
		rawJSON, err = runFlowdocSingleShotLLM(ctx, project, view, keyFiles, maxContextBytes, opts, cfg)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "flowdoc gen: %v\n", err)
		return 1
	}

	return finalizeFlowdoc(rawJSON, project, projectRoot, opts)
}

// chooseStrategy resolves the effective strategy for a real (non-dry-run)
// gen, applying the auto policy: agentic if an Anthropic API key is
// configured (config or env), single-shot otherwise. Explicit
// --strategy agentic|single-shot is passed through unchanged.
func chooseStrategy(opts flowdocGenOpts, cfg config.Config) (string, error) {
	switch opts.strategy {
	case "agentic", "single-shot":
		return opts.strategy, nil
	case "", "auto":
		if _, err := llm.ResolveAPIKey("anthropic", cfg.Providers); err == nil {
			return "agentic", nil
		}
		return "single-shot", nil
	default:
		return "", fmt.Errorf("unknown --strategy %q (want auto|agentic|single-shot)", opts.strategy)
	}
}

// runFlowdocSingleShotLLM is the Phase-3 single-shot path: assemble the
// tree listing + curated key-file contents into one user prompt, send a
// single ChatCompletion, return the raw text response.
func runFlowdocSingleShotLLM(ctx context.Context, project string, view flowdoc.RepoView, keyFiles []string, maxContextBytes int, opts flowdocGenOpts, cfg config.Config) (string, error) {
	model := opts.model
	if model == "" {
		model = cfg.Enrichment.Model
	}
	if model == "" {
		model = defaultFlowdocModel
	}
	cfg.Enrichment.Model = model
	cfg.Enrichment.Enabled = true

	provider, err := newProviderForFlowdoc(cfg)
	if err != nil {
		return "", fmt.Errorf("provider init: %w", err)
	}
	if provider == nil {
		return "", fmt.Errorf("no LLM provider available (check enrichment config)")
	}

	contextBlock, stats := flowdoc.BuildContext(view, keyFiles, maxContextBytes)
	userPrompt := buildUserPrompt(project, contextBlock)

	maxOutputTokens := opts.maxOutputTokens
	if maxOutputTokens == 0 {
		maxOutputTokens = defaultFlowdocOutputTokens
	}

	fmt.Printf("flowdoc gen: single-shot, %s walk, %d key files inlined (%d dropped), %d-byte context, model %s\n",
		view.Source, len(stats.Included), len(stats.Dropped), stats.TotalBytes, model)

	resp, err := provider.ChatCompletion(ctx, llm.Request{
		Model:       model,
		System:      flowdoc.Brief,
		UserPrompt:  userPrompt,
		Temperature: 0.0,
		JSONMode:    true,
		MaxTokens:   maxOutputTokens,
	})
	if err != nil {
		return "", fmt.Errorf("LLM call: %w", err)
	}
	return resp.Content, nil
}

// runFlowdocAgenticLLM is the Phase-4 agentic path: construct an
// AnthropicAgentic provider + a RepoView tool backend, hand the model
// the orienting prompt, and let it explore via read_file / grep /
// list_dir until it emits a terminal flows.json message. Returns the
// final assistant turn's text content.
func runFlowdocAgenticLLM(ctx context.Context, project string, view flowdoc.RepoView, opts flowdocGenOpts, cfg config.Config) (string, error) {
	model := resolveAgenticModel(opts.model, cfg.Enrichment.Model)

	provider, err := newAgenticProviderForFlowdoc(model, cfg)
	if err != nil {
		return "", fmt.Errorf("agentic provider init: %w", err)
	}
	if provider == nil {
		return "", fmt.Errorf("no agentic provider available")
	}

	tools, exec := flowdoc.NewRepoViewTools(view)

	maxIter := opts.maxIterations
	if maxIter == 0 {
		maxIter = defaultAgenticMaxIterations
	}
	maxOutputTokens := opts.maxOutputTokens
	if maxOutputTokens == 0 {
		maxOutputTokens = defaultFlowdocOutputTokens
	}

	fmt.Printf("flowdoc gen: agentic, %s walk, %d files enumerated, %d tools, max-iter %d, model %s\n",
		view.Source, view.Budget.FileCount, len(tools), maxIter, model)

	resp, err := provider.RunTools(ctx, llm.ToolsRequest{
		Model:  model,
		System: flowdoc.Brief + flowdoc.BriefAgenticAddendum,
		Messages: []llm.ToolsMessage{
			{Role: "user", Content: []llm.ContentBlock{{Type: "text", Text: buildAgenticUserPrompt(project)}}},
		},
		Tools:         tools,
		MaxIterations: maxIter,
		MaxTokens:     maxOutputTokens,
		ToolExecutor:  exec,
	})
	if err != nil {
		return "", fmt.Errorf("RunTools: %w", err)
	}
	if resp.StopReason == "max_tokens" {
		fmt.Fprintf(os.Stderr, "flowdoc gen: warning — agentic loop hit max-iterations (%d) cap; output may be incomplete; raise --max-iterations\n", maxIter)
	}
	return extractAgenticText(resp.Content), nil
}

// resolveAgenticModel picks the model for an agentic run: explicit
// --model wins, then the config's enrichment.model if it is an
// Anthropic ("claude-*") model, finally defaultAgenticModel.
func resolveAgenticModel(flagModel, cfgModel string) string {
	if flagModel != "" {
		return flagModel
	}
	if strings.HasPrefix(strings.ToLower(cfgModel), "claude") {
		return cfgModel
	}
	return defaultAgenticModel
}

// extractAgenticText concatenates the text content of an agentic
// response's final assistant turn. tool_use and other block types are
// ignored — the final flows.json should be in a text block.
func extractAgenticText(blocks []llm.ContentBlock) string {
	var sb strings.Builder
	for _, b := range blocks {
		if b.Type == "text" {
			sb.WriteString(b.Text)
		}
	}
	return sb.String()
}

// buildAgenticUserPrompt assembles the initial user message for an
// agentic run. Unlike the single-shot prompt, this carries no tree
// listing or file contents — the model is expected to discover them via
// list_dir / read_file / grep.
func buildAgenticUserPrompt(project string) string {
	return fmt.Sprintf(
		"Produce flows.json for the %q project. Use the read_file, grep, "+
			"and list_dir tools to explore the project — start with "+
			"list_dir(\"\") to see the repository root, then drill into "+
			"the directories and files that describe its workflows. Base "+
			"every node, path, and ref strictly on what the tools reveal; "+
			"do not invent paths or describe behavior you have not "+
			"verified. When you have enough to produce a complete "+
			"flows.json, return ONLY the JSON object as your final message.",
		project,
	)
}

// finalizeFlowdoc parses the raw LLM response, stamps required metadata,
// validates the FlowDoc, and writes doc/flows.json + doc/FLOWS.html.
// Shared by both strategies. Returns the process exit code.
func finalizeFlowdoc(rawJSON, project, projectRoot string, opts flowdocGenOpts) int {
	// Extract the first {...} object: strip a markdown fence; skip any
	// leading prose the model emits before the JSON ("Now I will produce
	// flows.json:"); use a streaming decoder so trailing commentary is
	// ignored. This is the agentic-mode "model wraps its output in
	// prose" failure mode the operator hit with haiku-4-5 on vibe-vault.
	candidate := stripJSONFence(rawJSON)
	if open := strings.IndexByte(candidate, '{'); open > 0 {
		candidate = candidate[open:]
	}
	var doc flowdoc.FlowDoc
	if err := json.NewDecoder(strings.NewReader(candidate)).Decode(&doc); err != nil {
		fmt.Fprintf(os.Stderr, "flowdoc gen: parse LLM response: %v\n", err)
		// A response that does not close its top-level object is the
		// signature of an output-token truncation (finding M3).
		if !strings.HasSuffix(strings.TrimSpace(candidate), "}") {
			fmt.Fprintln(os.Stderr, "flowdoc gen: response does not end with '}' — likely truncated; raise --max-output-tokens")
		}
		// Preserve the raw response for inspection — the operator
		// otherwise loses everything on a parse failure.
		brokenPath := filepath.Join(projectRoot, "doc", "flows.json.broken")
		if err := os.MkdirAll(filepath.Dir(brokenPath), 0o755); err == nil {
			if err := os.WriteFile(brokenPath, []byte(rawJSON), 0o644); err == nil {
				fmt.Fprintf(os.Stderr, "flowdoc gen: raw response saved to %s for inspection (%d bytes)\n", brokenPath, len(rawJSON))
			}
		}
		return 1
	}

	if doc.SchemaVersion == "" {
		doc.SchemaVersion = flowdoc.SchemaVersion
	}
	if doc.Project == "" {
		doc.Project = project
	}
	if doc.Generator == "" {
		doc.Generator = "vv flowdoc gen"
	}
	if doc.GeneratedAt == "" {
		doc.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
	}

	if err := flowdoc.Validate(&doc); err != nil {
		fmt.Fprintf(os.Stderr, "flowdoc gen: validate: %v\n", err)
		return 1
	}

	docDir := filepath.Join(projectRoot, "doc")
	if err := os.MkdirAll(docDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "flowdoc gen: mkdir %s: %v\n", docDir, err)
		return 1
	}

	jsonPath := filepath.Join(docDir, "flows.json")
	htmlPath := filepath.Join(docDir, "FLOWS.html")

	if err := writeFlowsJSON(jsonPath, &doc); err != nil {
		fmt.Fprintf(os.Stderr, "flowdoc gen: write %s: %v\n", jsonPath, err)
		return 1
	}
	if err := writeFlowsHTML(htmlPath, &doc); err != nil {
		fmt.Fprintf(os.Stderr, "flowdoc gen: write %s: %v\n", htmlPath, err)
		return 1
	}

	fmt.Printf("wrote %s\n", jsonPath)
	fmt.Printf("wrote %s\n", htmlPath)

	if opts.open {
		openInBrowser(htmlPath)
	}

	return 0
}

// runFlowdocDryRunSingleShot is the dry-run for the single-shot
// strategy: assembles the same context the real single-shot path would
// send (tree listing + inlined key files) and prints it plus the full
// system + user prompts. Default for --dry-run unless --strategy is
// explicitly set.
func runFlowdocDryRunSingleShot(project, projectRoot string, view flowdoc.RepoView, keyFiles []string, maxContextBytes int) int {
	contextBlock, stats := flowdoc.BuildContext(view, keyFiles, maxContextBytes)

	fmt.Println("flowdoc gen --dry-run (no LLM call)")
	fmt.Printf("  project:      %s\n", project)
	fmt.Printf("  root:         %s\n", projectRoot)
	fmt.Printf("  walk source:  %s\n", view.Source)
	fmt.Printf("  enumerated:   %d files, %d bytes (~%d est. tokens)\n",
		view.Budget.FileCount, view.Budget.TotalBytes, view.Budget.EstimatedTokens)
	fmt.Printf("  key files:    %d selected, %d inlined, %d dropped\n",
		len(keyFiles), len(stats.Included), len(stats.Dropped))
	fmt.Printf("  context:      %d bytes total, %d of file content (budget %d)\n",
		stats.TotalBytes, stats.ContentBytes, maxContextBytes)
	for _, p := range stats.Included {
		fmt.Printf("    + %s\n", p)
	}
	for _, p := range stats.Dropped {
		fmt.Printf("    - %s (dropped: over budget or unreadable)\n", p)
	}

	fmt.Println()
	fmt.Println("===== SYSTEM PROMPT (flowdoc.Brief) =====")
	fmt.Print(flowdoc.Brief) // Brief is newline-terminated already
	fmt.Println("===== USER PROMPT =====")
	fmt.Println(buildUserPrompt(project, contextBlock))
	return 0
}

// buildUserPrompt assembles the user message for `vv flowdoc gen`: the
// generation instruction followed by the repo context block produced by
// flowdoc.BuildContext. Shared by the real single-shot path and the
// single-shot dry-run so the inspection handle shows exactly what the
// LLM would receive.
func buildUserPrompt(project, contextBlock string) string {
	return fmt.Sprintf(
		"Produce flows.json for the %q project. The project's file tree "+
			"and the contents of its key files follow. Base every node, "+
			"path, and ref strictly on what you see below — do not invent "+
			"paths or describe behavior not present in the provided "+
			"sources. Return ONLY the JSON object.\n\n%s",
		project, contextBlock,
	)
}

// runFlowdocDryRunAgentic is the dry-run for the agentic strategy: it
// shows the tool catalogue, the operative model + iteration cap, and
// the system + initial-user prompt that would seed the RunTools loop —
// without making any LLM call. The multi-turn exploration itself is
// opaque without running the model, but the setup is fully inspectable.
func runFlowdocDryRunAgentic(project, projectRoot string, view flowdoc.RepoView, opts flowdocGenOpts) int {
	tools, _ := flowdoc.NewRepoViewTools(view)

	maxIter := opts.maxIterations
	if maxIter == 0 {
		maxIter = defaultAgenticMaxIterations
	}
	model := resolveAgenticModel(opts.model, "")

	fmt.Println("flowdoc gen --dry-run --strategy agentic (no LLM call)")
	fmt.Printf("  project:        %s\n", project)
	fmt.Printf("  root:           %s\n", projectRoot)
	fmt.Printf("  walk source:    %s\n", view.Source)
	fmt.Printf("  enumerated:     %d files, %d bytes (~%d est. tokens)\n",
		view.Budget.FileCount, view.Budget.TotalBytes, view.Budget.EstimatedTokens)
	fmt.Printf("  agentic model:  %s (override with --model)\n", model)
	fmt.Printf("  max iterations: %d (override with --max-iterations)\n", maxIter)
	fmt.Printf("  tools:          %d\n", len(tools))
	for _, t := range tools {
		fmt.Printf("    %s\n", t.Name)
	}

	fmt.Println()
	fmt.Println("===== SYSTEM PROMPT (Brief + agentic addendum) =====")
	fmt.Print(flowdoc.Brief) // Brief is newline-terminated
	fmt.Print(flowdoc.BriefAgenticAddendum)
	fmt.Println("===== INITIAL USER PROMPT =====")
	fmt.Println(buildAgenticUserPrompt(project))
	return 0
}

// parseFlowdocVerifyArgs parses the flag set for `vv flowdoc verify`. Only
// --project is accepted (verify is zero-LLM, so no --model/--open). Errors
// surface as a non-nil error so the caller controls the exit code.
func parseFlowdocVerifyArgs(args []string) (string, error) {
	var project string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--help", a == "-h", a == "-help":
			return "", fmt.Errorf("help requested")
		case a == "--project":
			if i+1 >= len(args) {
				return "", fmt.Errorf("--project requires a value")
			}
			project = args[i+1]
			i++
		case strings.HasPrefix(a, "--project="):
			project = strings.TrimPrefix(a, "--project=")
		default:
			return "", fmt.Errorf("unknown flag: %s", a)
		}
	}
	return project, nil
}

// runFlowdocVerify handles `vv flowdoc verify`. Returns the process exit
// code.
//
// Behavior:
//   - Resolves the project root the same way `gen` does (cwd → first
//     ancestor with a .git entry, falling back to cwd). --project is
//     accepted for parity but does not affect the doc/ location.
//   - Reads <project-root>/doc/flows.json. Missing → stderr hint, return 1.
//   - Unmarshals into flowdoc.FlowDoc. Parse failure → stderr, return 1.
//   - Runs flowdoc.Validate (structural) first. Failure → stderr, return 1.
//   - Runs flowdoc.VerifyRefs and prints FormatRefIssues.
//   - Any hard error → return 1; warnings-only → return 0; clean → print a
//     one-line summary and return 0.
func runFlowdocVerify(args []string) int {
	if _, err := parseFlowdocVerifyArgs(args); err != nil {
		if err.Error() == "help requested" {
			fmt.Fprintln(os.Stderr, "usage: vv flowdoc verify [--project <name>]")
			return 0
		}
		fmt.Fprintf(os.Stderr, "flowdoc verify: %v\n", err)
		fmt.Fprintln(os.Stderr, "usage: vv flowdoc verify [--project <name>]")
		return 2
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "flowdoc verify: getwd: %v\n", err)
		return 1
	}

	projectRoot := session.DetectProjectRoot(cwd)
	if projectRoot == "" {
		projectRoot = cwd
	}

	jsonPath := filepath.Join(projectRoot, "doc", "flows.json")
	raw, err := os.ReadFile(jsonPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "flowdoc verify: no doc/flows.json found (run 'vv flowdoc gen' first)")
		return 1
	}

	var doc flowdoc.FlowDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		fmt.Fprintf(os.Stderr, "flowdoc verify: parse %s: %v\n", jsonPath, err)
		return 1
	}

	if err := flowdoc.Validate(&doc); err != nil {
		fmt.Fprintf(os.Stderr, "flowdoc verify: %v\n", err)
		return 1
	}

	issues := flowdoc.VerifyRefs(&doc, projectRoot)
	report := flowdoc.FormatRefIssues(issues)

	hasError := false
	for _, i := range issues {
		if i.IsError() {
			hasError = true
			break
		}
	}

	if report != "" {
		fmt.Print(report)
	}

	if hasError {
		return 1
	}
	if len(issues) == 0 {
		fmt.Printf("flowdoc verify: %d flows, %d nodes, no drift\n", len(doc.Flows), len(doc.Nodes))
	}
	return 0
}

// stripJSONFence removes a leading ```json (or ```) fence and a trailing
// ``` fence from an LLM response. Anthropic and OpenAI both occasionally
// wrap their JSON-mode responses in markdown despite the JSON-mode hint;
// stripping is cheap and idempotent on already-bare JSON.
func stripJSONFence(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```json") {
		s = strings.TrimPrefix(s, "```json")
	} else if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
	}
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}

// writeFlowsJSON writes the doc as indented JSON for human-readable
// diffs. Indent is two spaces to match the existing golden fixture.
func writeFlowsJSON(path string, doc *flowdoc.FlowDoc) error {
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	// Trailing newline for POSIX-friendly diffs.
	b = append(b, '\n')
	return os.WriteFile(path, b, 0o644)
}

// writeFlowsHTML renders the doc to the embedded viewer template at path.
func writeFlowsHTML(path string, doc *flowdoc.FlowDoc) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return flowdoc.Render(f, doc)
}

// execCommandForOpen is the exec.Command indirection that openInBrowser
// uses to spawn xdg-open / open. Package-level var so tests can swap
// in a no-op or failing factory without invoking the real opener.
var execCommandForOpen = exec.Command

// openInBrowser fires xdg-open / open against the given path. Fire-and-
// return: we don't Wait on the child because the operator wants the
// viewer in their browser, not the CLI blocked behind it.
func openInBrowser(path string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		cmd = execCommandForOpen("xdg-open", path)
	case "darwin":
		cmd = execCommandForOpen("open", path)
	default:
		// Other OSes (windows, plan9, …) have no canonical open here.
		// Silent no-op rather than an error — --open is a convenience.
		return
	}
	// Detach stdin/stdout/stderr from the parent so the spawned process
	// doesn't keep our pipes alive.
	_ = cmd.Start()
}
