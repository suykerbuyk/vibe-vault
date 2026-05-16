// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package flowdoc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/suykerbuyk/vibe-vault/internal/llm"
)

// This file is the agentic-LLM tool backend for `vv flowdoc gen` (the
// flowdoc-gen-source-ingestion task, Phase 4). NewRepoViewTools returns
// a tool catalogue and a dispatcher backed entirely by the Phase-1
// RepoView accessors — no new core code, the H1 "revert plus bounded
// additions" premise. The single-shot prompt-builder (Phase 3) and the
// agentic tool backend (here) share the same RepoView source of truth.

// grepHitCap bounds the number of matches grep returns to the model in
// a single response. Above this the tool truncates and sets a flag so
// the model can narrow its pattern or path_prefix. The cap keeps a wide
// regex from blowing a context window and forces the model to think
// about specificity.
const grepHitCap = 50

// NewRepoViewTools returns the tool catalogue (Tools) and the dispatcher
// (Executor) that drive an llm.AgenticProvider.RunTools loop over a
// RepoView. Three tools are exposed:
//
//   - read_file(path)               → contents of one kept file
//   - grep(pattern, [path_prefix])  → up to grepHitCap matching lines
//   - list_dir(path, [recursive])   → directory entries, derived from the flat listing
//
// All three delegate to existing RepoView accessors (ReadFile, Search,
// and the Files listing) so the agentic path inherits the same
// filter-class semantics as the single-shot path: gitignored / gitlink /
// committed-noise files are invisible to the model, and oversize files
// can be listed but never read.
func NewRepoViewTools(view RepoView) ([]llm.ToolSpec, llm.ToolExecutor) {
	tools := []llm.ToolSpec{
		{
			Name:        "read_file",
			Description: "Read the contents of one file from the project tree. The path argument must be a repo-relative path from the tree listing (e.g. \"internal/foo/bar.go\"). Refuses files over 1 MiB and files that appear to be binary. Returns the file contents on success.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"repo-relative path to a file present in the project tree"}},"required":["path"]}`),
		},
		{
			Name:        "grep",
			Description: "Search the project source for a Go-regexp pattern across every kept file. Returns up to 50 matching lines with their file path and line number; the response includes a truncated flag when the cap is hit. Use grep to locate a symbol's definition or callers without reading every file. Narrow scope with the optional path_prefix argument (e.g. \"internal/mcp/\").",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string","description":"Go regexp pattern (RE2 syntax)"},"path_prefix":{"type":"string","description":"optional path prefix; only matching files under this prefix are scanned"}},"required":["pattern"]}`),
		},
		{
			Name:        "list_dir",
			Description: "List the entries directly under a project directory. With recursive=true, returns all descendants of the directory instead of just direct children. Pass path=\"\" or path=\".\" to list the project root. Directory entries are returned with a trailing slash; file entries are bare basenames (non-recursive) or full relative paths (recursive). Use this to discover what is in a subsystem before deciding which files to read.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"repo-relative directory path; \"\" or \".\" means the project root"},"recursive":{"type":"boolean","description":"when true, return all descendants; default false"}}}`),
		},
	}

	exec := func(name string, input json.RawMessage) (json.RawMessage, bool) {
		switch name {
		case "read_file":
			return execReadFile(view, input)
		case "grep":
			return execGrep(view, input)
		case "list_dir":
			return execListDir(view, input)
		default:
			return errorPayload(fmt.Sprintf("unknown tool: %q", name)), true
		}
	}
	return tools, exec
}

// errorPayload marshals a uniform { "error": "<msg>" } JSON object. The
// agentic provider forwards it as the tool_result payload with the
// wire-level is_error flag set, so the model can recover.
func errorPayload(msg string) json.RawMessage {
	b, _ := json.Marshal(struct {
		Error string `json:"error"`
	}{Error: msg})
	return b
}

// execReadFile handles the read_file tool: unmarshal the input, delegate
// to RepoView.ReadFile (which enforces membership + size cap), reject
// binaries on a NUL-byte heuristic, return the content as JSON.
func execReadFile(view RepoView, input json.RawMessage) (json.RawMessage, bool) {
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return errorPayload(fmt.Sprintf("invalid input: %v", err)), true
	}
	if args.Path == "" {
		return errorPayload("path is required"), true
	}
	data, err := view.ReadFile(args.Path)
	if err != nil {
		return errorPayload(err.Error()), true
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return errorPayload(fmt.Sprintf("%q appears to be binary; refusing to read", args.Path)), true
	}
	out, _ := json.Marshal(struct {
		Path    string `json:"path"`
		Size    int    `json:"size"`
		Content string `json:"content"`
	}{Path: args.Path, Size: len(data), Content: string(data)})
	return out, false
}

// execGrep handles the grep tool: compile the pattern via RepoView.Search,
// optionally filter the resulting hits to those under path_prefix, cap to
// grepHitCap, return as JSON with a truncated flag.
func execGrep(view RepoView, input json.RawMessage) (json.RawMessage, bool) {
	var args struct {
		Pattern    string `json:"pattern"`
		PathPrefix string `json:"path_prefix"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return errorPayload(fmt.Sprintf("invalid input: %v", err)), true
	}
	if args.Pattern == "" {
		return errorPayload("pattern is required"), true
	}
	hits, err := view.Search(args.Pattern)
	if err != nil {
		return errorPayload(err.Error()), true
	}

	type grepHit struct {
		Path string `json:"path"`
		Line int    `json:"line"`
		Text string `json:"text"`
	}
	filtered := make([]grepHit, 0, len(hits))
	for _, h := range hits {
		if args.PathPrefix != "" && !strings.HasPrefix(h.Path, args.PathPrefix) {
			continue
		}
		filtered = append(filtered, grepHit(h))
	}
	truncated := false
	if len(filtered) > grepHitCap {
		filtered = filtered[:grepHitCap]
		truncated = true
	}
	out, _ := json.Marshal(struct {
		Pattern   string    `json:"pattern"`
		Hits      []grepHit `json:"hits"`
		Total     int       `json:"total"`
		Truncated bool      `json:"truncated"`
	}{Pattern: args.Pattern, Hits: filtered, Total: len(filtered), Truncated: truncated})
	return out, false
}

// execListDir handles the list_dir tool: derive a directory listing from
// the flat RepoView.Files. Non-recursive collapses descendant dirs to
// their first segment with a trailing slash; recursive returns all
// descendant relative paths.
func execListDir(view RepoView, input json.RawMessage) (json.RawMessage, bool) {
	var args struct {
		Path      string `json:"path"`
		Recursive bool   `json:"recursive"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return errorPayload(fmt.Sprintf("invalid input: %v", err)), true
	}
	dirPath := strings.Trim(args.Path, "/")
	if dirPath == "." {
		dirPath = ""
	}
	var prefix string
	if dirPath != "" {
		prefix = dirPath + "/"
	}

	seen := make(map[string]struct{})
	var entries []string
	for _, f := range view.Files {
		if prefix != "" && !strings.HasPrefix(f.Path, prefix) {
			continue
		}
		rest := f.Path[len(prefix):]
		if rest == "" {
			continue
		}
		var entry string
		if args.Recursive {
			entry = rest
		} else if slash := strings.IndexByte(rest, '/'); slash >= 0 {
			entry = rest[:slash] + "/"
		} else {
			entry = rest
		}
		if _, ok := seen[entry]; !ok {
			seen[entry] = struct{}{}
			entries = append(entries, entry)
		}
	}
	sort.Strings(entries)

	out, _ := json.Marshal(struct {
		Path      string   `json:"path"`
		Recursive bool     `json:"recursive"`
		Entries   []string `json:"entries"`
		Count     int      `json:"count"`
	}{Path: args.Path, Recursive: args.Recursive, Entries: entries, Count: len(entries)})
	return out, false
}
