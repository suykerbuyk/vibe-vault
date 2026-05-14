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
	"strings"
	"time"

	"github.com/suykerbuyk/vibe-vault/internal/config"
	"github.com/suykerbuyk/vibe-vault/internal/flowdoc"
	"github.com/suykerbuyk/vibe-vault/internal/llm"
	"github.com/suykerbuyk/vibe-vault/internal/session"
)

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

// runFlowdoc dispatches `vv flowdoc <verb>`. Returns the process exit
// code; main.go's switch wires this through directly.
func runFlowdoc(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: vv flowdoc <gen> [...]")
		return 2
	}
	switch args[0] {
	case "gen":
		return runFlowdocGen(args[1:])
	case "--help", "-help", "-h", "help":
		fmt.Fprintln(os.Stderr, "usage: vv flowdoc <gen> [...]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "subcommands:")
		fmt.Fprintln(os.Stderr, "  gen   Generate doc/flows.json + doc/FLOWS.html via LLM")
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown flowdoc verb: %s\n", args[0])
		return 2
	}
}

// flowdocGenOpts holds parsed --project / --model / --open flag values.
// Extracted as a struct so tests can assert parse outcomes without
// driving the full LLM happy path.
type flowdocGenOpts struct {
	project string
	model   string
	open    bool
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
			fmt.Fprintln(os.Stderr, "usage: vv flowdoc gen [--project <name>] [--model <name>] [--open]")
			return 0
		}
		fmt.Fprintf(os.Stderr, "flowdoc gen: %v\n", err)
		fmt.Fprintln(os.Stderr, "usage: vv flowdoc gen [--project <name>] [--model <name>] [--open]")
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

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "flowdoc gen: load config: %v\n", err)
		return 1
	}

	// Model resolution: flag > config > hard default. Override
	// cfg.Enrichment.Model so the provider's NewX constructor sees the
	// caller-selected model without needing per-request plumbing —
	// matches what every other vv subcommand does.
	model := opts.model
	if model == "" {
		model = cfg.Enrichment.Model
	}
	if model == "" {
		model = defaultFlowdocModel
	}
	cfg.Enrichment.Model = model
	// Force enrichment on for this invocation — `vv flowdoc gen` is a
	// direct LLM action; the operator wouldn't invoke it without
	// expecting an LLM call. Keeping the original config's API key /
	// provider selection intact, we only flip the enabled bit.
	cfg.Enrichment.Enabled = true

	provider, err := newProviderForFlowdoc(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "flowdoc gen: provider init: %v\n", err)
		return 1
	}
	if provider == nil {
		fmt.Fprintln(os.Stderr, "flowdoc gen: no LLM provider available (check enrichment config)")
		return 1
	}

	userPrompt := fmt.Sprintf(
		"Produce flows.json for the %q project at %s. Walk the source "+
			"tree, templates, and MCP prompts. Return ONLY the JSON object.",
		project, projectRoot,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	resp, err := provider.ChatCompletion(ctx, llm.Request{
		Model:       model,
		System:      flowdoc.Brief,
		UserPrompt:  userPrompt,
		Temperature: 0.0,
		JSONMode:    true,
		MaxTokens:   16384,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "flowdoc gen: LLM call: %v\n", err)
		return 1
	}

	raw := stripJSONFence(resp.Content)

	var doc flowdoc.FlowDoc
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		fmt.Fprintf(os.Stderr, "flowdoc gen: parse LLM response: %v\n", err)
		return 1
	}

	// Stamp metadata the LLM may have missed or stubbed. Generator string
	// is canonical so downstream tooling can recognize provenance.
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
