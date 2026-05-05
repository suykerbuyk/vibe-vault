// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import "testing"

// TestClassifyWrapShape covers the 7 cases from the
// wrap-mcp-offload-state-collector-and-preflight plan's DoD checklist:
//
//  1. commits non-empty + tasks-added empty            → fresh-feature
//  2. commits non-empty + tasks-added non-empty        → fresh-feature
//  3. commits empty + tasks-added non-empty            → planning
//  4. commits empty + tasks-added empty                → bookkeeping
//  5. vault-dirty + iter_n−1-already-in-iter-md + plain
//     → writes-already-landed (preempts bookkeeping)
//  6. vault-dirty + iter_n−1-already-in-iter-md + tasks-added
//     → writes-already-landed (preempts planning)
//  7. vault-dirty + iter_n−1-already-in-iter-md + commits
//     → writes-already-landed (preempts fresh-feature)
//
// The classifier is a pure function; we exercise it via direct
// CollectWrapStateResult literals — no I/O needed.
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
		{
			name: "case 5: writes-already-landed preempts bookkeeping",
			in: CollectWrapStateResult{
				VaultHasUncommittedWrites:          true,
				IterNMinusOneAlreadyInIterationsMD: true,
				CommitsSinceLastIter:               []CommitInfo{},
				TaskDeltas:                         TaskDeltas{},
			},
			want: ShapeWritesAlreadyLanded,
		},
		{
			name: "case 6: writes-already-landed preempts planning",
			in: CollectWrapStateResult{
				VaultHasUncommittedWrites:          true,
				IterNMinusOneAlreadyInIterationsMD: true,
				CommitsSinceLastIter:               []CommitInfo{},
				TaskDeltas:                         TaskDeltas{Added: []string{"new-task"}},
			},
			want: ShapeWritesAlreadyLanded,
		},
		{
			name: "case 7: writes-already-landed preempts fresh-feature",
			in: CollectWrapStateResult{
				VaultHasUncommittedWrites:          true,
				IterNMinusOneAlreadyInIterationsMD: true,
				CommitsSinceLastIter:               []CommitInfo{{SHA: "abc123", Subject: "feat: x"}},
				TaskDeltas:                         TaskDeltas{},
			},
			want: ShapeWritesAlreadyLanded,
		},
		// Additional defense-in-depth cases: ensure the
		// short-circuit only fires when BOTH conditions hold.
		{
			name: "vault-dirty alone (no iter-N-1) does not preempt",
			in: CollectWrapStateResult{
				VaultHasUncommittedWrites:          true,
				IterNMinusOneAlreadyInIterationsMD: false,
				CommitsSinceLastIter:               []CommitInfo{},
				TaskDeltas:                         TaskDeltas{},
			},
			want: ShapeBookkeeping,
		},
		{
			name: "iter-N-1 alone (vault clean) does not preempt",
			in: CollectWrapStateResult{
				VaultHasUncommittedWrites:          false,
				IterNMinusOneAlreadyInIterationsMD: true,
				CommitsSinceLastIter:               []CommitInfo{{SHA: "abc"}},
				TaskDeltas:                         TaskDeltas{},
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
