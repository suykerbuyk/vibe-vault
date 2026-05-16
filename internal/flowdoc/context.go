// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package flowdoc

import (
	"fmt"
	"path"
	"sort"
	"strings"
)

// This file assembles the LLM prompt context block for `vv flowdoc gen`
// (the flowdoc-gen-source-ingestion task, Phase 3). BuildContext turns a
// RepoView plus a SelectKeyFiles selection into the text appended to the
// user prompt: a full file-tree listing followed by the contents of as
// many key files as a byte budget allows. This is the input-gathering
// step the iter-236 spike did with Read/Glob/Grep tools and the shipped
// single-shot `gen` command never had.

// DefaultContextBudgetBytes caps the file-content portion of a
// generation prompt. The iter-236 spike ran well at ~120 K input
// tokens; at the ~4-char/token heuristic that is ~480 KB, so 600 KB of
// file content leaves headroom for the tree listing and the brief. The
// tree listing itself is always included regardless of this budget.
const DefaultContextBudgetBytes = 600 * 1024

// ContextStats records what BuildContext included and dropped — the
// data behind the `--dry-run` inspection handle and the Phase-3
// measurement record.
type ContextStats struct {
	TreeFileCount int      // files named in the tree listing
	Included      []string // key files whose contents were inlined
	Dropped       []string // key files dropped (over budget or unreadable)
	ContentBytes  int      // total bytes of inlined file content
	TotalBytes    int      // total size of the assembled context block
}

// BuildContext assembles the prompt context block for a generation run:
// a full file-tree listing followed by the contents of the selected key
// files, in priority order, up to budgetBytes of file content.
//
// The tree listing is ALWAYS included in full — it is cheap and is the
// model's map of the project. Key-file contents are added in priority
// order: build manifests, then entry points, then top-level docs;
// within a class the smallest files first, so one large file cannot
// crowd out several high-signal small ones. When a file's content would
// push the running total past budgetBytes it is skipped and recorded in
// ContextStats.Dropped, and the packer moves on to the next (smaller or
// lower-priority) file. A key file whose content cannot be read
// (RepoView.ReadFile error — oversize, vanished) is likewise dropped.
//
// budgetBytes <= 0 means "no budget" — every readable key file is
// included.
func BuildContext(view RepoView, keyFiles []string, budgetBytes int) (string, ContextStats) {
	stats := ContextStats{TreeFileCount: len(view.Files)}

	var b strings.Builder
	b.WriteString("# Project file tree\n\n")
	for _, f := range view.Files {
		b.WriteString(f.Path)
		b.WriteByte('\n')
	}

	// Order key files by (class, size, path): highest-signal, cheapest
	// files are inlined first when the budget is tight.
	sizeByPath := make(map[string]int64, len(view.Files))
	for _, f := range view.Files {
		sizeByPath[f.Path] = f.Size
	}
	ordered := append([]string(nil), keyFiles...)
	sort.Slice(ordered, func(i, j int) bool {
		ci, cj := keyFileClass(ordered[i]), keyFileClass(ordered[j])
		if ci != cj {
			return ci < cj
		}
		if si, sj := sizeByPath[ordered[i]], sizeByPath[ordered[j]]; si != sj {
			return si < sj
		}
		return ordered[i] < ordered[j]
	})

	b.WriteString("\n# Selected file contents\n")
	for _, rel := range ordered {
		data, err := view.ReadFile(rel)
		if err != nil {
			stats.Dropped = append(stats.Dropped, rel)
			continue
		}
		if budgetBytes > 0 && stats.ContentBytes+len(data) > budgetBytes {
			stats.Dropped = append(stats.Dropped, rel)
			continue
		}
		fmt.Fprintf(&b, "\n===== FILE: %s =====\n%s\n===== END %s =====\n", rel, data, rel)
		stats.Included = append(stats.Included, rel)
		stats.ContentBytes += len(data)
	}
	if len(stats.Included) == 0 {
		b.WriteString("\n(no file contents available within the budget)\n")
	}

	out := b.String()
	stats.TotalBytes = len(out)
	return out, stats
}

// keyFileClass ranks a key file for budget-priority ordering: build
// manifests (0) outrank entry points (1) outrank top-level docs (2).
// The default arm is the top-level-doc case — SelectKeyFiles produces
// only those three categories, so anything not a manifest or entry
// point is a doc.
func keyFileClass(rel string) int {
	base := path.Base(rel)
	switch {
	case isManifest(base):
		return 0
	case isEntryPoint(base):
		return 1
	default:
		return 2
	}
}
