// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package check

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/suykerbuyk/vibe-vault/internal/flowdoc"
	"github.com/suykerbuyk/vibe-vault/internal/session"
)

// CheckFlowdoc reports whether the project's doc/flows.json is in sync
// with the source tree. It is a warn-only check — it NEVER emits Fail —
// so it cannot change `vv check`'s exit code or block `/restart` or
// `/wrap`. The four possible outcomes are:
//
//   - No doc/flows.json on disk → Warn, "no flows.json" (cosmetic; the
//     artifact is written by `vv flowdoc gen`, which is not run live).
//   - flows.json parses, flowdoc.Validate passes, and flowdoc.VerifyRefs
//     returns zero error-level issues → Pass, "<N> flows, <M> nodes, no
//     drift". Weak-match warnings do NOT count as drift — only
//     RefIssue.IsError() issues do.
//   - flows.json fails to parse or fails flowdoc.Validate → Warn,
//     "invalid flows.json; run vv flowdoc verify".
//   - flows.json parses and validates but flowdoc.VerifyRefs returns one
//     or more error-level issues → Warn, "<N> drift; run vv flowdoc
//     verify".
//
// Project-root resolution mirrors cmd/vv/flowdoc.go's runFlowdocVerify:
// the first ancestor of cwd with a .git entry, falling back to cwd
// itself. That same root is doc/flows.json's parent AND the repoRoot
// VerifyRefs resolves refs against. Returns nil when cwd is empty —
// matching the project-scoped-check convention used elsewhere in this
// package.
func CheckFlowdoc(cwd string) *Result {
	if cwd == "" {
		return nil
	}

	projectRoot := session.DetectProjectRoot(cwd)
	if projectRoot == "" {
		projectRoot = cwd
	}

	jsonPath := filepath.Join(projectRoot, "doc", "flows.json")
	raw, err := os.ReadFile(jsonPath)
	if err != nil {
		return &Result{
			Name:   "flowdoc",
			Status: Warn,
			Detail: "no flows.json",
		}
	}

	var doc flowdoc.FlowDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return &Result{
			Name:   "flowdoc",
			Status: Warn,
			Detail: "invalid flows.json; run vv flowdoc verify",
		}
	}

	if err := flowdoc.Validate(&doc); err != nil {
		return &Result{
			Name:   "flowdoc",
			Status: Warn,
			Detail: "invalid flows.json; run vv flowdoc verify",
		}
	}

	issues := flowdoc.VerifyRefs(&doc, projectRoot)
	errCount := 0
	for _, i := range issues {
		if i.IsError() {
			errCount++
		}
	}

	if errCount > 0 {
		return &Result{
			Name:   "flowdoc",
			Status: Warn,
			Detail: fmt.Sprintf("%d drift; run vv flowdoc verify", errCount),
		}
	}

	return &Result{
		Name:   "flowdoc",
		Status: Pass,
		Detail: fmt.Sprintf("%d flows, %d nodes, no drift", len(doc.Flows), len(doc.Nodes)),
	}
}
