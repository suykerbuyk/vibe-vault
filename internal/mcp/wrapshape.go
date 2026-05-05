// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

// WrapShape names a work-unit shape per the rules in
// templates/agentctx/commands/wrap.md. Used by ClassifyWrapShape and
// surfaced as the `shape` field of vv_collect_wrap_state's response
// (Commit 2 will register that tool; Commit 1 is helpers-only).
type WrapShape string

const (
	ShapeFreshFeature        WrapShape = "fresh-feature"
	ShapePlanning            WrapShape = "planning"
	ShapeBookkeeping         WrapShape = "bookkeeping"
	ShapeWritesAlreadyLanded WrapShape = "writes-already-landed"
)

// CommitInfo summarizes a commit between the last-iter anchor and HEAD.
type CommitInfo struct {
	SHA     string `json:"sha"`
	Subject string `json:"subject"`
}

// TaskDeltas reports task-folder transitions since the last wrap. Per the
// C3-v6 fix, deltas are computed via project-repo snapshot comparison
// (.vibe-vault/last-tasks-snapshot.json), not git-history walking.
type TaskDeltas struct {
	Added     []string `json:"added"`
	Retired   []string `json:"retired"`
	Cancelled []string `json:"cancelled"`
}

// TestCounts captures the headline counts parsed out of doc/TESTING.md.
// Warning carries any non-fatal parse-failure detail (empty on success).
type TestCounts struct {
	Unit        int    `json:"unit"`
	Integration int    `json:"integration"`
	Lint        int    `json:"lint"`
	Warning     string `json:"warning,omitempty"`
}

// CollectWrapStateResult is the JSON shape returned by vv_collect_wrap_state
// (registered in Commit 2). Defined here so the classifier and helper
// functions in tools_collect_wrap_state.go can reference it without
// forward-declaration churn.
type CollectWrapStateResult struct {
	IterN                              int          `json:"iter_n"`
	Branch                             string       `json:"branch"`
	LastIterAnchorSha                  string       `json:"last_iter_anchor_sha,omitempty"`
	IterNMinusOneAlreadyInIterationsMD bool         `json:"iter_n_minus_one_already_in_iterations_md"`
	CommitsSinceLastIter               []CommitInfo `json:"commits_since_last_iter"`
	FilesChanged                       []string     `json:"files_changed"`
	TaskDeltas                         TaskDeltas   `json:"task_deltas"`
	TestCounts                         TestCounts   `json:"test_counts"`
	VaultHasUncommittedWrites          bool         `json:"vault_has_uncommitted_writes"`
	ProjectHasUncommittedWrites        bool         `json:"project_has_uncommitted_writes"`
	Shape                              WrapShape    `json:"shape"`
}

// ClassifyWrapShape returns the work-unit shape per the rules in
// templates/agentctx/commands/wrap.md. Pure function: no I/O, no
// state. The caller materializes all input fields before calling.
//
// Short-circuit precedence: writes-already-landed wins over the other
// three shapes per the "prefer most restrictive" tie-break (the iter-N-1
// narrative is already in iterations.md AND the vault has uncommitted
// writes — i.e., the wrap's writes have landed but the commit didn't).
// After that, fresh-feature (commits since anchor) beats planning
// (tasks added) beats bookkeeping (no signals).
func ClassifyWrapShape(state CollectWrapStateResult) WrapShape {
	if state.VaultHasUncommittedWrites && state.IterNMinusOneAlreadyInIterationsMD {
		return ShapeWritesAlreadyLanded
	}
	if len(state.CommitsSinceLastIter) > 0 {
		return ShapeFreshFeature
	}
	if len(state.TaskDeltas.Added) > 0 {
		return ShapePlanning
	}
	return ShapeBookkeeping
}
