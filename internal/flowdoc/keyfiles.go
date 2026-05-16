// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package flowdoc

import (
	"path"
	"slices"
	"sort"
	"strings"
)

// This file is the discovery-driven key-file selector for `vv flowdoc
// gen` (see the flowdoc-gen-source-ingestion task, finding H3). The
// generator always sends the model the full repo tree listing;
// SelectKeyFiles picks the smaller set of files whose *contents* are
// additionally inlined into the prompt. Selection keys off file names
// and locations actually present in the RepoView — never a hardcoded
// project layout — so it works across language shapes.

// langProfile bundles one language's discovery signals. Adding a
// language is appending a langProfile; the selection loop never changes
// (the H3 "language-tagged, localized" requirement).
type langProfile struct {
	name        string   // human label, e.g. "go", "python"
	manifests   []string // exact basenames marking this language's build config
	entryPoints []string // exact basenames that are conventional entry points
}

// langProfiles is the discovery table. Order is not significant — a file
// is selected if ANY profile matches it.
var langProfiles = []langProfile{
	{
		name:        "go",
		manifests:   []string{"go.mod", "go.work"},
		entryPoints: []string{"main.go"},
	},
	{
		name:        "c-cmake",
		manifests:   []string{"CMakeLists.txt", "meson.build", "configure.ac"},
		entryPoints: []string{"main.c", "main.cc", "main.cpp", "main.cxx"},
	},
	{
		name:        "python",
		manifests:   []string{"pyproject.toml", "setup.py", "setup.cfg"},
		entryPoints: []string{"__main__.py", "main.py", "app.py"},
	},
	{
		name:        "rust",
		manifests:   []string{"Cargo.toml"},
		entryPoints: []string{"main.rs"},
	},
	{
		name:        "node",
		manifests:   []string{"package.json"},
		entryPoints: []string{"index.js", "index.ts", "main.js", "main.ts"},
	},
}

// crossLangManifests are build-driver files not tied to a single
// language. Kept separate so langProfiles stays language-pure.
var crossLangManifests = []string{"Makefile", "GNUmakefile"}

// SelectKeyFiles picks, from a RepoView's enumerated listing, the
// high-signal subset whose file *contents* belong in a flow-doc
// generation prompt. The full tree listing is always sent to the model;
// this selects which files are additionally inlined.
//
// Selection is discovery-driven (the flowdoc-gen-source-ingestion H3
// finding): it keys off file names and locations actually present in
// view, never a hardcoded project layout, so it works across Go, C,
// Python, Rust and Node shapes alike. Three signal categories are
// unioned:
//
//   - build manifests — go.mod, CMakeLists.txt, package.json, … : the
//     project's declared type, targets and module layout.
//   - entry points    — main.*, __main__.py, index.* : where execution
//     begins.
//   - top-level docs   — repo-root README* and *.md : author intent.
//
// Files larger than maxFileBytes are skipped: their content cannot be
// inlined anyway (RepoView.ReadFile refuses them), so selecting them
// would be a dead entry. The result is sorted, deduplicated, and a
// strict subset of the paths in view.Files. Budget enforcement —
// dropping low-priority contents to fit a token ceiling — is
// deliberately NOT done here; that is the caller's job (Phase 3).
func SelectKeyFiles(view RepoView) []string {
	seen := make(map[string]struct{})
	var out []string
	add := func(p string) {
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	for _, f := range view.Files {
		if f.Size > maxFileBytes {
			continue
		}
		base := path.Base(f.Path)
		switch {
		case isManifest(base), isEntryPoint(base), isTopLevelDoc(f.Path):
			add(f.Path)
		}
	}
	sort.Strings(out)
	return out
}

// isManifest reports whether base is a recognized build-manifest
// basename — language-specific (langProfiles) or cross-language
// (crossLangManifests).
func isManifest(base string) bool {
	for _, p := range langProfiles {
		if slices.Contains(p.manifests, base) {
			return true
		}
	}
	return slices.Contains(crossLangManifests, base)
}

// isEntryPoint reports whether base is a recognized entry-point basename
// for any language profile.
func isEntryPoint(base string) bool {
	for _, p := range langProfiles {
		if slices.Contains(p.entryPoints, base) {
			return true
		}
	}
	return false
}

// isTopLevelDoc reports whether rel is a repo-root README* or *.md file.
// "Top-level" means no path separator — docs nested under doc/ or docs/
// are left to the tree listing; only the root-level intent docs
// (README, ARCHITECTURE.md, DESIGN.md, …) are inlined. The match is
// case-insensitive.
func isTopLevelDoc(rel string) bool {
	if strings.Contains(rel, "/") {
		return false
	}
	lower := strings.ToLower(rel)
	return strings.HasSuffix(lower, ".md") || strings.HasPrefix(lower, "readme")
}
