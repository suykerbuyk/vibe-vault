// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

// File wrapbundle.go centralises the skeleton/prose-split types and helpers
// for the wrap pipeline (Decision 26 + H2-v3).
//
// A WrapSkeleton holds the orchestrator-collected facts for a wrap iteration:
// iter number, project, files changed, decisions, and the slug-only edit
// plan (threads to open/replace/close, carried adds/removes, task
// retirements). It carries NO prose. The skeleton is persisted via
// internal/wrapbundlecache so subsequent escalation tiers can reuse the same
// facts without re-collecting them.
//
// A WrapBundle is the fully-prose-filled artifact ready for apply: each
// thread/carried block has a body string, the iteration block has
// narrative, and a commit message + capture-session payload are present.
//
// BuildSkeleton(facts SkeletonFacts) WrapSkeleton — pure, no IO. Used by
// vv_prepare_wrap_skeleton.
//
// FillBundle(skeleton WrapSkeleton, prose ProseFields) WrapBundle — pure,
// no IO. Used by vv_synthesize_wrap_bundle (and, per OQ-5, by the Phase 3c
// dispatch handler via direct Go call rather than re-entering the MCP loop).
//
// The bundle types reused below (WrapBundle, BundleThreadBlock, etc.) live
// in tools_synthesize_wrap.go and remain on-disk-compatible with
// wrapmetrics output.
package mcp

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
)

// WrapSkeleton is the orchestrator-facts payload for a wrap iteration. It
// contains NO prose — each opened/replaced thread, each carried-add, and
// each iteration block is described only by its slug or coordinates. Bodies
// are filled later via FillBundle.
type WrapSkeleton struct {
	Iter                 int                      `json:"iter"`
	Project              string                   `json:"project"`
	FilesChanged         []string                 `json:"files_changed"`
	TestCountDelta       int                      `json:"test_count_delta"` // simple int sum; the structured object surgical apply uses lives elsewhere.
	Decisions            []string                 `json:"decisions"`
	ResumeThreadBlocks   []SkeletonThreadOpen     `json:"resume_thread_blocks"`
	ResumeThreadsReplace []SkeletonThreadReplace  `json:"resume_threads_to_replace"` // H2-v3
	ResumeThreadsToClose []SkeletonThreadClose    `json:"resume_threads_to_close"`
	CarriedChangesAdd    []SkeletonCarriedAdd     `json:"carried_changes_add"`
	CarriedChangesRemove []SkeletonCarriedRemove  `json:"carried_changes_remove"`
	TaskRetirements      []SkeletonTaskRetirement `json:"task_retirements"`
	SkeletonTimestamp    string                   `json:"skeleton_timestamp"`
}

// SkeletonThreadOpen identifies a thread to open: slug + optional anchor.
type SkeletonThreadOpen struct {
	Slug         string `json:"slug"`
	AnchorBefore string `json:"anchor_before,omitempty"`
	AnchorAfter  string `json:"anchor_after,omitempty"`
}

// SkeletonThreadReplace identifies a thread whose body will be replaced
// (H2-v3). The body itself is supplied later via ProseFields.
type SkeletonThreadReplace struct {
	Slug string `json:"slug"`
}

// SkeletonThreadClose identifies a thread to remove.
type SkeletonThreadClose struct {
	Slug string `json:"slug"`
}

// SkeletonCarriedAdd identifies a carried-forward bullet to add: slug +
// title only. The body is filled via ProseFields.
type SkeletonCarriedAdd struct {
	Slug  string `json:"slug"`
	Title string `json:"title"`
}

// SkeletonCarriedRemove identifies a carried-forward bullet to remove.
type SkeletonCarriedRemove struct {
	Slug string `json:"slug"`
}

// SkeletonTaskRetirement records a task to mark complete with an optional
// note.
type SkeletonTaskRetirement struct {
	Task string `json:"task"`
	Note string `json:"note,omitempty"`
}

// SkeletonFacts is the typed input to BuildSkeleton. It mirrors the
// vv_prepare_wrap_skeleton tool's JSON input.
type SkeletonFacts struct {
	Iter                 int
	Project              string
	FilesChanged         []string
	TestCountDelta       int
	Decisions            []string
	ResumeThreadBlocks   []SkeletonThreadOpen
	ResumeThreadsReplace []SkeletonThreadReplace
	ResumeThreadsToClose []SkeletonThreadClose
	CarriedChangesAdd    []SkeletonCarriedAdd
	CarriedChangesRemove []SkeletonCarriedRemove
	TaskRetirements      []SkeletonTaskRetirement
}

// ProseFields is the typed input to FillBundle. It carries the executor
// outputs that get filled into the skeleton to produce a complete bundle.
type ProseFields struct {
	IterationNarrative  string
	IterationTitle      string
	ProseBody           string
	CommitSubject       string
	Date                string
	ThreadBodies        map[string]string // slug → body, for opened + replaced threads
	CarriedBodies       map[string]string // slug → body
	CaptureSummary      string
	CaptureTag          string
	CaptureDecisions    []string
	CaptureFilesChanged []string
	CaptureOpenThreads  []string
}

// BuildSkeleton returns a WrapSkeleton populated from facts. It performs no
// IO. The SkeletonTimestamp is set to the current UTC RFC3339 instant.
func BuildSkeleton(facts SkeletonFacts) WrapSkeleton {
	return WrapSkeleton{
		Iter:                 facts.Iter,
		Project:              facts.Project,
		FilesChanged:         cloneStrings(facts.FilesChanged),
		TestCountDelta:       facts.TestCountDelta,
		Decisions:            cloneStrings(facts.Decisions),
		ResumeThreadBlocks:   cloneSkeletonOpens(facts.ResumeThreadBlocks),
		ResumeThreadsReplace: cloneSkeletonReplaces(facts.ResumeThreadsReplace),
		ResumeThreadsToClose: cloneSkeletonCloses(facts.ResumeThreadsToClose),
		CarriedChangesAdd:    cloneSkeletonCarriedAdds(facts.CarriedChangesAdd),
		CarriedChangesRemove: cloneSkeletonCarriedRemoves(facts.CarriedChangesRemove),
		TaskRetirements:      cloneSkeletonTaskRetirements(facts.TaskRetirements),
		SkeletonTimestamp:    time.Now().UTC().Format(time.RFC3339),
	}
}

// FillBundle constructs a WrapBundle from a skeleton and prose fields. It
// performs no IO. Bodies are looked up by slug; missing bodies leave the
// resulting field empty (Phase 3b's QC tool surfaces this).
//
// The returned bundle uses the existing WrapBundle / BundleThreadBlock /
// BundleCarriedAdd / BundleCaptureSession types so the on-disk wrapmetrics
// fingerprints remain compatible.
func FillBundle(skeleton WrapSkeleton, prose ProseFields) WrapBundle {
	date := prose.Date
	if date == "" {
		date = time.Now().Format("2006-01-02")
	}

	// Build the iteration block (narrative + heading).
	iterContent := BuildIterationBlock(skeleton.Iter, prose.IterationTitle, prose.IterationNarrative, date, "")

	// Build the commit message body.
	proseBody := prose.ProseBody
	if proseBody == "" {
		proseBody = prose.IterationNarrative
	}
	filesSection := buildFilesSection(skeleton.FilesChanged)
	commitContent := renderCommitMsg(prose.CommitSubject, proseBody, filesSection, skeleton.TestCountDelta, 0, 0, skeleton.Iter)

	// Build resume_thread_blocks. Bodies come from prose.ThreadBodies by slug.
	threadBlocks := make([]BundleThreadBlock, 0, len(skeleton.ResumeThreadBlocks))
	for _, tb := range skeleton.ResumeThreadBlocks {
		body := prose.ThreadBodies[tb.Slug]
		threadBlocks = append(threadBlocks, BundleThreadBlock{
			Position:    threadOpenPosition(tb),
			Slug:        tb.Slug,
			Body:        body,
			SynthSHA256: fingerprintString(tb.Slug + "\x00" + body),
		})
	}

	// Build resume_threads_to_replace.
	threadReplace := make([]BundleThreadReplace, 0, len(skeleton.ResumeThreadsReplace))
	for _, tr := range skeleton.ResumeThreadsReplace {
		body := prose.ThreadBodies[tr.Slug]
		threadReplace = append(threadReplace, BundleThreadReplace{
			Slug:        tr.Slug,
			Body:        body,
			SynthSHA256: fingerprintString(tr.Slug + "\x00" + body),
		})
	}

	// Build resume_threads_to_close.
	threadClose := make([]BundleThreadClose, 0, len(skeleton.ResumeThreadsToClose))
	for _, tc := range skeleton.ResumeThreadsToClose {
		threadClose = append(threadClose, BundleThreadClose{
			Slug:        tc.Slug,
			SynthSHA256: fingerprintString(tc.Slug),
		})
	}

	// Build carried_changes.add.
	carriedAdd := make([]BundleCarriedAdd, 0, len(skeleton.CarriedChangesAdd))
	for _, ca := range skeleton.CarriedChangesAdd {
		body := prose.CarriedBodies[ca.Slug]
		carriedAdd = append(carriedAdd, BundleCarriedAdd{
			Slug:        ca.Slug,
			Title:       ca.Title,
			Body:        body,
			SynthSHA256: fingerprintString(ca.Slug + "\x00" + ca.Title + "\x00" + body),
		})
	}

	// Build carried_changes.remove.
	carriedRemove := make([]BundleCarriedRemove, 0, len(skeleton.CarriedChangesRemove))
	for _, cr := range skeleton.CarriedChangesRemove {
		carriedRemove = append(carriedRemove, BundleCarriedRemove{
			Slug:        cr.Slug,
			SynthSHA256: fingerprintString(cr.Slug),
		})
	}

	// Build capture_session.
	cc := BundleCaptureContent{
		Summary:      prose.CaptureSummary,
		Tag:          prose.CaptureTag,
		Decisions:    cloneStrings(prose.CaptureDecisions),
		FilesChanged: cloneStrings(prose.CaptureFilesChanged),
		OpenThreads:  cloneStrings(prose.CaptureOpenThreads),
	}
	if cc.Tag == "" {
		cc.Tag = "implementation"
	}
	captureSHA, fpErr := fingerprintJSON(cc)
	if fpErr != nil {
		captureSHA = "sha256-error:" + fpErr.Error()
	}

	return WrapBundle{
		IterationBlock: BundleFieldWithContent{
			Content:     iterContent,
			SynthSHA256: fingerprintString(iterContent),
		},
		CommitMsg: BundleFieldWithContent{
			Content:     commitContent,
			SynthSHA256: fingerprintString(commitContent),
		},
		ResumeThreadBlocks:    threadBlocks,
		ResumeThreadsReplace:  threadReplace,
		ResumeThreadsToClose:  threadClose,
		CarriedChanges: BundleCarriedChanges{
			Add:    carriedAdd,
			Remove: carriedRemove,
		},
		CaptureSession: BundleCaptureSession{
			Content:     cc,
			SynthSHA256: captureSHA,
		},
		SynthTimestamp: time.Now().UTC().Format(time.RFC3339),
		Iteration:      skeleton.Iter,
	}
}

// SkeletonSHA256 returns the hex-encoded SHA-256 of bytes.
func SkeletonSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// threadOpenPosition translates the skeleton anchor pair into the position
// map used by mdutil.InsertSubsection. Anchor-after takes precedence over
// anchor-before; if neither is set, position defaults to top.
func threadOpenPosition(s SkeletonThreadOpen) map[string]string {
	if s.AnchorAfter != "" {
		return map[string]string{"mode": "after", "anchor_slug": s.AnchorAfter}
	}
	if s.AnchorBefore != "" {
		return map[string]string{"mode": "before", "anchor_slug": s.AnchorBefore}
	}
	return map[string]string{"mode": "top"}
}

// buildFilesSection returns the bullet-list rendering of files for the
// commit message body.
func buildFilesSection(files []string) string {
	if len(files) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, f := range files {
		sb.WriteString("- ")
		sb.WriteString(f)
		sb.WriteString("\n")
	}
	return sb.String()
}

// cloneStrings returns a shallow copy of in (or nil for empty input).
func cloneStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func cloneSkeletonOpens(in []SkeletonThreadOpen) []SkeletonThreadOpen {
	if len(in) == 0 {
		return nil
	}
	out := make([]SkeletonThreadOpen, len(in))
	copy(out, in)
	return out
}

func cloneSkeletonReplaces(in []SkeletonThreadReplace) []SkeletonThreadReplace {
	if len(in) == 0 {
		return nil
	}
	out := make([]SkeletonThreadReplace, len(in))
	copy(out, in)
	return out
}

func cloneSkeletonCloses(in []SkeletonThreadClose) []SkeletonThreadClose {
	if len(in) == 0 {
		return nil
	}
	out := make([]SkeletonThreadClose, len(in))
	copy(out, in)
	return out
}

func cloneSkeletonCarriedAdds(in []SkeletonCarriedAdd) []SkeletonCarriedAdd {
	if len(in) == 0 {
		return nil
	}
	out := make([]SkeletonCarriedAdd, len(in))
	copy(out, in)
	return out
}

func cloneSkeletonCarriedRemoves(in []SkeletonCarriedRemove) []SkeletonCarriedRemove {
	if len(in) == 0 {
		return nil
	}
	out := make([]SkeletonCarriedRemove, len(in))
	copy(out, in)
	return out
}

func cloneSkeletonTaskRetirements(in []SkeletonTaskRetirement) []SkeletonTaskRetirement {
	if len(in) == 0 {
		return nil
	}
	out := make([]SkeletonTaskRetirement, len(in))
	copy(out, in)
	return out
}
