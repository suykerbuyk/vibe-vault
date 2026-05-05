// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import "testing"

// TestClassifyWrapShape covers the four base cells of the
// (commits-empty, task-added-empty) Cartesian partition that survives
// after the writes-already-landed shape was retired. The classifier is
// a pure function; we exercise it via direct CollectWrapStateResult
// literals — no I/O needed.
//
// Cases:
//
//  1. commits non-empty + tasks-added empty            → fresh-feature
//  2. commits non-empty + tasks-added non-empty        → fresh-feature
//  3. commits empty + tasks-added non-empty            → planning
//  4. commits empty + tasks-added empty                → bookkeeping
//
// Plus three regression-locks documenting that vault-dirty alone, or
// vault-dirty combined with any of the other shape-positive signals,
// does NOT short-circuit to a separate shape — `writes-already-landed`
// was retired. Collision avoidance lives in `vv_append_iteration`'s
// content-addressable idempotency contract, not the classifier.
func TestClassifyWrapShape(t *testing.T) {
	cases := []struct {
		name string
		in   CollectWrapStateResult
		want WrapShape
	}{
		{
			name: "case 1: commits non-empty, tasks-added empty",
			in: CollectWrapStateResult{
				CommitsSinceLastIter: []CommitInfo{{SHA: "abc123", Subject: "feat: x"}},
				TaskDeltas:           TaskDeltas{},
			},
			want: ShapeFreshFeature,
		},
		{
			name: "case 2: commits non-empty, tasks-added non-empty",
			in: CollectWrapStateResult{
				CommitsSinceLastIter: []CommitInfo{{SHA: "abc123", Subject: "feat: x"}},
				TaskDeltas:           TaskDeltas{Added: []string{"new-task"}},
			},
			want: ShapeFreshFeature,
		},
		{
			name: "case 3: commits empty, tasks-added non-empty",
			in: CollectWrapStateResult{
				CommitsSinceLastIter: []CommitInfo{},
				TaskDeltas:           TaskDeltas{Added: []string{"new-task"}},
			},
			want: ShapePlanning,
		},
		{
			name: "case 4: commits empty, tasks-added empty",
			in: CollectWrapStateResult{
				CommitsSinceLastIter: []CommitInfo{},
				TaskDeltas:           TaskDeltas{},
			},
			want: ShapeBookkeeping,
		},
		// Regression-locks: vault-dirty does not change classification.
		// Pre-deletion the (vault-dirty, iter-n-1-present) AND short-
		// circuited to writes-already-landed and misfired four times
		// against legitimate fresh-feature / planning / bookkeeping
		// iters. Post-deletion the classifier reports the operator's
		// actual shape regardless of vault dirty state.
		{
			name: "regression: vault-dirty does not preempt bookkeeping",
			in: CollectWrapStateResult{
				VaultHasUncommittedWrites: true,
				CommitsSinceLastIter:      []CommitInfo{},
				TaskDeltas:                TaskDeltas{},
			},
			want: ShapeBookkeeping,
		},
		{
			name: "regression: vault-dirty does not preempt planning",
			in: CollectWrapStateResult{
				VaultHasUncommittedWrites: true,
				CommitsSinceLastIter:      []CommitInfo{},
				TaskDeltas:                TaskDeltas{Added: []string{"new-task"}},
			},
			want: ShapePlanning,
		},
		{
			name: "regression: vault-dirty does not preempt fresh-feature",
			in: CollectWrapStateResult{
				VaultHasUncommittedWrites: true,
				CommitsSinceLastIter:      []CommitInfo{{SHA: "abc123", Subject: "feat: x"}},
				TaskDeltas:                TaskDeltas{},
			},
			want: ShapeFreshFeature,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyWrapShape(tc.in)
			if got != tc.want {
				t.Errorf("ClassifyWrapShape(...) = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestClassifyWrapShape_InTheWildReplays replays the three in-the-wild
// misfires (iters 220, 222, 223) against the post-deletion classifier
// and asserts each iter classifies as the operator's actual shape, not
// the retired `writes-already-landed`.
//
// Source of conditions: classifier-writes-already-landed-fix Draft v2
// "In-the-wild instances" section. The shared trigger across all three
// was vault_has_uncommitted_writes=true combined with iter_n >= 2 (so
// pre-deletion the predicate `iter_n_minus_one_already_in_iterations_md`
// was true by construction).
func TestClassifyWrapShape_InTheWildReplays(t *testing.T) {
	cases := []struct {
		name string
		in   CollectWrapStateResult
		want WrapShape
		why  string
	}{
		{
			name: "iter 220 replay: carried_remove resume edits + bookkeeping iter",
			in: CollectWrapStateResult{
				IterN:                     220,
				VaultHasUncommittedWrites: true, // resume.md edits from carried_remove
				CommitsSinceLastIter:      []CommitInfo{},
				TaskDeltas:                TaskDeltas{},
			},
			// Iter 220 was operator-classified `fresh-feature` per the
			// plan body; pre-deletion the classifier returned
			// `writes-already-landed`. Post-deletion no commits + no
			// task adds → bookkeeping; the operator could still
			// override-up to fresh-feature but the classifier's job is
			// to report the mechanical signal.
			want: ShapeBookkeeping,
			why:  "iter 220 had no commits + no task adds beyond the vault narrative; mechanical shape is bookkeeping",
		},
		{
			name: "iter 222 replay: /review-plan v3->v4->v5 task rewrites + planning iter",
			in: CollectWrapStateResult{
				IterN:                     222,
				VaultHasUncommittedWrites: true, // task file rewrites via vv_vault_write
				CommitsSinceLastIter:      []CommitInfo{},
				TaskDeltas:                TaskDeltas{},
			},
			// Iter 222 was operator-classified `bookkeeping` (per the
			// plan body's narrative) — task rewrites of an existing
			// task aren't `task_deltas.added`, so the mechanical
			// signal is bookkeeping. Pre-deletion the classifier
			// returned `writes-already-landed`.
			want: ShapeBookkeeping,
			why:  "iter 222 rewrote an existing task (no add); mechanical shape is bookkeeping",
		},
		{
			name: "iter 223 replay: 6 fresh-feature commits + dirty vault from iter 222 unflushed",
			in: CollectWrapStateResult{
				IterN:                     223,
				VaultHasUncommittedWrites: true, // iter 222's never-pushed row + plan + resume
				CommitsSinceLastIter: []CommitInfo{
					{SHA: "c1", Subject: "feat(sessionsource): add interface"},
					{SHA: "c2", Subject: "test(sessionsource): conformance"},
					{SHA: "c3", Subject: "refactor(cmd/vv): runZedWatch routes"},
					{SHA: "c4", Subject: "docs(sessionsource): wire DESIGN entry"},
					{SHA: "c5", Subject: "test(sessionsource): routing fork"},
					{SHA: "c6", Subject: "feat(cmd/vv): SessionSource registry"},
				},
				TaskDeltas: TaskDeltas{},
			},
			// Iter 223 shipped 6 fresh-feature commits via /execute-plan
			// for session-source-interface. Operator-correct shape was
			// fresh-feature; pre-deletion the classifier returned
			// `writes-already-landed`.
			want: ShapeFreshFeature,
			why:  "iter 223 had 6 commits via /execute-plan; mechanical shape is fresh-feature",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyWrapShape(tc.in)
			if got != tc.want {
				t.Errorf("ClassifyWrapShape(...) = %q, want %q (%s)", got, tc.want, tc.why)
			}
			if string(got) == "writes-already-landed" {
				t.Errorf("classifier still emits retired writes-already-landed shape — deletion regression")
			}
		})
	}
}
