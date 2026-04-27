// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"strings"
	"testing"
	"time"
)

func sampleSkeletonFacts() SkeletonFacts {
	return SkeletonFacts{
		Iter:           42,
		Project:        "myproject",
		FilesChanged:   []string{"a.go", "b.go"},
		TestCountDelta: 7,
		Decisions:      []string{"chose A over B"},
		ResumeThreadBlocks: []SkeletonThreadOpen{
			{Slug: "thread-1", AnchorAfter: "anchor-x"},
			{Slug: "thread-2"},
		},
		ResumeThreadsReplace: []SkeletonThreadReplace{
			{Slug: "old-thread"},
		},
		ResumeThreadsToClose: []SkeletonThreadClose{
			{Slug: "to-close"},
		},
		CarriedChangesAdd: []SkeletonCarriedAdd{
			{Slug: "carry-1", Title: "First carry"},
		},
		CarriedChangesRemove: []SkeletonCarriedRemove{
			{Slug: "stale-carry"},
		},
		TaskRetirements: []SkeletonTaskRetirement{
			{Task: "old-task", Note: "shipped"},
		},
	}
}

func TestBuildSkeleton_PopulatesAllFields(t *testing.T) {
	facts := sampleSkeletonFacts()
	sk := BuildSkeleton(facts)

	if sk.Iter != 42 {
		t.Errorf("Iter=%d, want 42", sk.Iter)
	}
	if sk.Project != "myproject" {
		t.Errorf("Project=%q, want myproject", sk.Project)
	}
	if len(sk.FilesChanged) != 2 || sk.FilesChanged[0] != "a.go" {
		t.Errorf("FilesChanged=%v", sk.FilesChanged)
	}
	if sk.TestCountDelta != 7 {
		t.Errorf("TestCountDelta=%d, want 7", sk.TestCountDelta)
	}
	if len(sk.Decisions) != 1 || sk.Decisions[0] != "chose A over B" {
		t.Errorf("Decisions=%v", sk.Decisions)
	}
	if len(sk.ResumeThreadBlocks) != 2 {
		t.Fatalf("ResumeThreadBlocks len=%d, want 2", len(sk.ResumeThreadBlocks))
	}
	if sk.ResumeThreadBlocks[0].AnchorAfter != "anchor-x" {
		t.Errorf("anchor_after=%q", sk.ResumeThreadBlocks[0].AnchorAfter)
	}
	if len(sk.ResumeThreadsReplace) != 1 || sk.ResumeThreadsReplace[0].Slug != "old-thread" {
		t.Errorf("ResumeThreadsReplace=%v", sk.ResumeThreadsReplace)
	}
	if len(sk.ResumeThreadsToClose) != 1 || sk.ResumeThreadsToClose[0].Slug != "to-close" {
		t.Errorf("ResumeThreadsToClose=%v", sk.ResumeThreadsToClose)
	}
	if len(sk.CarriedChangesAdd) != 1 || sk.CarriedChangesAdd[0].Title != "First carry" {
		t.Errorf("CarriedChangesAdd=%v", sk.CarriedChangesAdd)
	}
	if len(sk.CarriedChangesRemove) != 1 || sk.CarriedChangesRemove[0].Slug != "stale-carry" {
		t.Errorf("CarriedChangesRemove=%v", sk.CarriedChangesRemove)
	}
	if len(sk.TaskRetirements) != 1 || sk.TaskRetirements[0].Task != "old-task" {
		t.Errorf("TaskRetirements=%v", sk.TaskRetirements)
	}
	if sk.SkeletonTimestamp == "" {
		t.Error("SkeletonTimestamp empty")
	}
}

func TestBuildSkeleton_TimestampFormat(t *testing.T) {
	sk := BuildSkeleton(SkeletonFacts{Iter: 1, Project: "p"})
	if _, err := time.Parse(time.RFC3339, sk.SkeletonTimestamp); err != nil {
		t.Errorf("SkeletonTimestamp not RFC3339: %v (got %q)", err, sk.SkeletonTimestamp)
	}
}

func TestFillBundle_PopulatesProse(t *testing.T) {
	sk := BuildSkeleton(sampleSkeletonFacts())
	prose := ProseFields{
		IterationNarrative: "Did some great work.",
		IterationTitle:     "Phase 3a wrap",
		ProseBody:          "Phase 3a wrap. Body.",
		CommitSubject:      "feat(mcp): test",
		Date:               "2026-04-25",
		ThreadBodies: map[string]string{
			"thread-1":   "body-of-thread-1",
			"thread-2":   "body-of-thread-2",
			"old-thread": "replacement body",
		},
		CarriedBodies: map[string]string{
			"carry-1": "carry body content",
		},
		CaptureSummary:      "Summary line.",
		CaptureTag:          "implementation",
		CaptureDecisions:    []string{"chose A"},
		CaptureFilesChanged: []string{"a.go"},
		CaptureOpenThreads:  []string{"thread-1", "thread-2"},
	}

	bundle := FillBundle(sk, prose)

	// Iteration block + commit msg.
	if !strings.Contains(bundle.IterationBlock.Content, "### Iteration 42 — Phase 3a wrap (2026-04-25)") {
		t.Errorf("iteration heading wrong: %s", bundle.IterationBlock.Content)
	}
	if !strings.Contains(bundle.CommitMsg.Content, "feat(mcp): test") {
		t.Errorf("commit subject missing: %s", bundle.CommitMsg.Content)
	}

	// Threads.
	if len(bundle.ResumeThreadBlocks) != 2 {
		t.Fatalf("threads len=%d, want 2", len(bundle.ResumeThreadBlocks))
	}
	bySlug := map[string]string{}
	for _, b := range bundle.ResumeThreadBlocks {
		bySlug[b.Slug] = b.Body
	}
	if bySlug["thread-1"] != "body-of-thread-1" || bySlug["thread-2"] != "body-of-thread-2" {
		t.Errorf("thread bodies=%v", bySlug)
	}

	// thread positions: thread-1 has anchor_after, thread-2 defaults to top.
	for _, b := range bundle.ResumeThreadBlocks {
		if b.Slug == "thread-1" && b.Position["mode"] != "after" {
			t.Errorf("thread-1 position=%v", b.Position)
		}
		if b.Slug == "thread-2" && b.Position["mode"] != "top" {
			t.Errorf("thread-2 position=%v", b.Position)
		}
	}

	// Replace.
	if len(bundle.ResumeThreadsReplace) != 1 {
		t.Fatalf("replace len=%d", len(bundle.ResumeThreadsReplace))
	}
	if bundle.ResumeThreadsReplace[0].Body != "replacement body" {
		t.Errorf("replace body=%q", bundle.ResumeThreadsReplace[0].Body)
	}

	// Carried add.
	if len(bundle.CarriedChanges.Add) != 1 || bundle.CarriedChanges.Add[0].Body != "carry body content" {
		t.Errorf("carried add=%v", bundle.CarriedChanges.Add)
	}

	// Capture summary.
	if bundle.CaptureSession.Content.Summary != "Summary line." {
		t.Errorf("capture summary=%q", bundle.CaptureSession.Content.Summary)
	}
	if bundle.SynthTimestamp == "" {
		t.Error("SynthTimestamp empty")
	}
	if bundle.Iteration != 42 {
		t.Errorf("Iteration=%d, want 42", bundle.Iteration)
	}
}

func TestFillBundle_EmptyProseLeavesEmptyStrings(t *testing.T) {
	sk := BuildSkeleton(SkeletonFacts{
		Iter:    1,
		Project: "p",
		ResumeThreadBlocks: []SkeletonThreadOpen{
			{Slug: "no-body-thread"},
		},
		CarriedChangesAdd: []SkeletonCarriedAdd{
			{Slug: "no-body-carry", Title: "Title only"},
		},
	})
	bundle := FillBundle(sk, ProseFields{})

	if len(bundle.ResumeThreadBlocks) != 1 {
		t.Fatalf("threads len=%d", len(bundle.ResumeThreadBlocks))
	}
	if bundle.ResumeThreadBlocks[0].Body != "" {
		t.Errorf("expected empty body, got %q", bundle.ResumeThreadBlocks[0].Body)
	}
	if len(bundle.CarriedChanges.Add) != 1 {
		t.Fatalf("carried add len=%d", len(bundle.CarriedChanges.Add))
	}
	if bundle.CarriedChanges.Add[0].Body != "" {
		t.Errorf("expected empty body, got %q", bundle.CarriedChanges.Add[0].Body)
	}
}

func TestFillBundle_ThreadReplaceCoverage(t *testing.T) {
	sk := BuildSkeleton(SkeletonFacts{
		Iter:    1,
		Project: "p",
		ResumeThreadsReplace: []SkeletonThreadReplace{
			{Slug: "alpha"},
			{Slug: "beta"},
		},
	})
	prose := ProseFields{
		ThreadBodies: map[string]string{
			"alpha": "alpha-body",
			"beta":  "beta-body",
		},
	}
	bundle := FillBundle(sk, prose)
	if len(bundle.ResumeThreadsReplace) != 2 {
		t.Fatalf("replace len=%d, want 2", len(bundle.ResumeThreadsReplace))
	}
	bySlug := map[string]string{}
	for _, r := range bundle.ResumeThreadsReplace {
		bySlug[r.Slug] = r.Body
		if r.SynthSHA256 == "" {
			t.Errorf("replace[%s].SynthSHA256 empty", r.Slug)
		}
	}
	if bySlug["alpha"] != "alpha-body" || bySlug["beta"] != "beta-body" {
		t.Errorf("replace bodies=%v", bySlug)
	}
}

func TestSkeletonSHA256_Stability(t *testing.T) {
	a := SkeletonSHA256([]byte("hello"))
	b := SkeletonSHA256([]byte("hello"))
	if a != b {
		t.Errorf("SkeletonSHA256 not deterministic: %s vs %s", a, b)
	}
	c := SkeletonSHA256([]byte("world"))
	if a == c {
		t.Errorf("expected different sha for different input")
	}
}
