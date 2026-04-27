// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

// File tools_quality_check.go implements vv_wrap_quality_check (Phase 3b of
// the wrap-model-tiering plan). The tool is a server-side validator that
// runs Decision 8's escalation triggers against a wrap-executor's proposed
// outputs BEFORE the apply step writes anything to vault. Per H3-v2, QC
// runs read-only — it dry-runs vault lookups for ambiguity detection but
// does NOT mutate any file.
//
// The four trigger checks performed here:
//
//  1. multi_match_ambiguity: each thread/carried mutation in the bundle is
//     dry-run looked up against live vault state. Inserts must NOT find an
//     existing slug; replace/remove must find exactly one. >1 anchors of
//     the same slug or 0 anchors for a replace/remove fires the trigger.
//  2. mutation_count_mismatch: derived expected count from the skeleton
//     vs actual count from the filled bundle. Reuses the helpers from
//     tools_apply_wrap_bundle.go to keep the formula in one place
//     (Decision 8, +3 constant for iter+commit_msg+capture).
//  3. semantic_presence_failure: every paragraph in the iteration narrative
//     must cite at least one of: commit SHA, file path, function name, or
//     decision number. The narrative as a whole must include a commit-
//     range span (sha..sha). This catches generic AI-slop narratives.
//  4. commit_subject_invalid: the commit subject is non-empty and not in
//     the rejection list (WIP, wip, fix, update, change, edit).
//
// Two other Decision 8 triggers (mcp_tool_error_after_retry, missing_
// terminal_signal) fire from the Phase 3c dispatch loop, not from QC, and
// are documented but not implemented here.
package mcp

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/suykerbuyk/vibe-vault/internal/config"
	"github.com/suykerbuyk/vibe-vault/internal/mdutil"
)

// qcFailure is one entry in the failures array returned to callers.
type qcFailure struct {
	TriggerID string `json:"trigger_id"`
	Detail    string `json:"detail"`
}

// qcResult is the JSON shape returned by vv_wrap_quality_check.
type qcResult struct {
	Passed   bool        `json:"passed"`
	Failures []qcFailure `json:"failures"`
}

// Trigger ID constants — kept in one place so the dispatch loop in Phase 3c
// can match against the same identifiers without string drift.
const (
	triggerMultiMatchAmbiguity   = "multi_match_ambiguity"
	triggerMutationCountMismatch = "mutation_count_mismatch"
	triggerSemanticPresence      = "semantic_presence_failure"
	triggerCommitSubjectInvalid  = "commit_subject_invalid"
)

// rejectedCommitSubjects is the lazy-subject blocklist from Decision 8.
// Comparison is exact (case-sensitive) per plan; the list itself contains
// both "WIP" and "wip" so common variants are caught.
var rejectedCommitSubjects = map[string]bool{
	"WIP":    true,
	"wip":    true,
	"fix":    true,
	"update": true,
	"change": true,
	"edit":   true,
}

// Compiled regexes used by the semantic presence check.
//
// File-path regex note: the wrap-model-tiering plan originally specified
// `/[a-zA-Z0-9_./-]+` (with a leading slash). Sampling the live
// VibeVault/Projects/vibe-vault/agentctx/iterations.md confirms that
// existing iteration narratives cite files like `agentctx/resume.md` and
// `internal/llm/anthropic.go` WITHOUT a leading slash — applying the
// plan's regex verbatim would fail every recent narrative. We loosen to
// match a slash-bearing path with a typical source-file extension. This
// preserves the spirit of the check (catch generic narratives that name
// no concrete artifact) while not breaking on stylistic choices.
var (
	qcRegexCommitSHA   = regexp.MustCompile(`[a-f0-9]{7,40}`)
	qcRegexCommitRange = regexp.MustCompile(`[a-f0-9]{7,40}\.\.[a-f0-9]{7,40}`)
	qcRegexFilePath    = regexp.MustCompile(`(?:^|[\s` + "`" + `(\[])/?[a-zA-Z0-9_.-]+/[a-zA-Z0-9_./-]+\.(?:go|md|toml|json|yaml|yml|sh|ts|tsx|js|py|txt|sum|mod)\b`)
	qcRegexFuncName    = regexp.MustCompile(`\w+\(\)`)
	qcRegexDecisionNum = regexp.MustCompile(`(?:D\d+|#\d{2,3})`)
)

// NewWrapQualityCheckTool creates the vv_wrap_quality_check tool.
//
// Input: {project?, skeleton_handle: {iter, skeleton_path, skeleton_sha256},
// outputs: {iteration_narrative, prose_body, commit_subject, thread_bodies,
// carried_bodies, capture_summary, capture_tag?, capture_decisions?,
// capture_files_changed?, capture_open_threads?}}.
//
// Output: {passed: bool, failures: [{trigger_id, detail}, ...]}.
//
// The handler is read-only: it loads the skeleton via the cache (sha256-
// verified, same as the apply tool), reconstructs the bundle in memory via
// FillBundle, and dry-runs lookups against live resume.md content for the
// ambiguity check. It never writes.
func NewWrapQualityCheckTool(cfg config.Config) Tool {
	return Tool{
		Definition: ToolDef{
			Name: "vv_wrap_quality_check",
			Description: "Validate wrap-executor proposed outputs against Decision 8 escalation " +
				"triggers BEFORE the apply step writes anything to vault. Runs four " +
				"checks (multi_match_ambiguity, mutation_count_mismatch, " +
				"semantic_presence_failure, commit_subject_invalid) and returns " +
				"{passed, failures[]}. Read-only against vault state — uses the same " +
				"skeleton_handle compare-and-set guard as vv_apply_wrap_bundle_by_handle " +
				"and dry-runs the resume.md / carried-forward lookups without writing. " +
				"All failing triggers are accumulated; the dispatch loop wants to see " +
				"every failure in one shot for telemetry.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"project": {
						"type": "string",
						"description": "Project name. If omitted, detected from working directory."
					},
					"skeleton_handle": {
						"type": "object",
						"description": "{iter, skeleton_path, skeleton_sha256} returned by vv_prepare_wrap_skeleton.",
						"properties": {
							"iter":            {"type": "integer"},
							"skeleton_path":   {"type": "string"},
							"skeleton_sha256": {"type": "string"}
						},
						"required": ["iter", "skeleton_path"]
					},
					"outputs": {
						"type": "object",
						"description": "Executor-supplied prose. Same shape as vv_apply_wrap_bundle_by_handle.outputs.",
						"properties": {
							"iteration_narrative":   {"type": "string"},
							"iteration_title":       {"type": "string"},
							"prose_body":            {"type": "string"},
							"commit_subject":        {"type": "string"},
							"date":                  {"type": "string"},
							"thread_bodies":         {"type": "object"},
							"carried_bodies":        {"type": "object"},
							"capture_summary":       {"type": "string"},
							"capture_tag":           {"type": "string"},
							"capture_decisions":     {"type": "array", "items": {"type": "string"}},
							"capture_files_changed": {"type": "array", "items": {"type": "string"}},
							"capture_open_threads":  {"type": "array", "items": {"type": "string"}}
						}
					}
				},
				"required": ["skeleton_handle", "outputs"]
			}`),
		},
		Handler: func(params json.RawMessage) (string, error) {
			var args struct {
				Project        string         `json:"project"`
				SkeletonHandle SkeletonHandle `json:"skeleton_handle"`
				Outputs        proseInputArgs `json:"outputs"`
			}
			if len(params) > 0 {
				if err := json.Unmarshal(params, &args); err != nil {
					return "", fmt.Errorf("invalid arguments: %w", err)
				}
			}

			skeleton, err := loadSkeletonByHandle(args.SkeletonHandle)
			if err != nil {
				return "", err
			}

			bundle := FillBundle(skeleton, args.Outputs.toProseFields())

			var failures []qcFailure

			// Check 1: multi-match ambiguity (vault dry-run, READ-ONLY).
			// We resolve the project here because the ambiguity check needs
			// to read the vault file. If the project cannot be resolved we
			// skip the check rather than failing the QC run on a CWD
			// ambiguity unrelated to the executor outputs.
			project, projectErr := resolveProject(args.Project)
			if projectErr == nil {
				resumeContent, _, readErr := readResume(cfg, project)
				if readErr != nil {
					// resume.md missing / unreadable: surface as ambiguity
					// failure so the executor cannot accidentally win when
					// the vault is in an unexpected state. No write happens.
					failures = append(failures, qcFailure{
						TriggerID: triggerMultiMatchAmbiguity,
						Detail:    fmt.Sprintf("read resume.md: %v", readErr),
					})
				} else {
					failures = append(failures, dryRunAmbiguityCheck(resumeContent, bundle)...)
				}
			}

			// Check 2: mutation count mismatch.
			// Reuses expectedMutationCount / actualMutationCount from
			// tools_apply_wrap_bundle.go so the +3 formula stays in one place.
			expected := expectedMutationCount(skeleton)
			actual := actualMutationCount(bundle)
			if expected != actual {
				failures = append(failures, qcFailure{
					TriggerID: triggerMutationCountMismatch,
					Detail: fmt.Sprintf(
						"expected=%d, actual=%d, breakdown: iter=1 thread_insert=%d thread_replace=%d thread_remove=%d carried_add=%d carried_remove=%d commit_msg=1 capture=1",
						expected, actual,
						len(skeleton.ResumeThreadBlocks),
						len(skeleton.ResumeThreadsReplace),
						len(skeleton.ResumeThreadsToClose),
						len(skeleton.CarriedChangesAdd),
						len(skeleton.CarriedChangesRemove),
					),
				})
			}

			// Check 3: semantic presence in the narrative.
			failures = append(failures, semanticPresenceFailures(args.Outputs.IterationNarrative)...)

			// Check 4: commit subject validity.
			failures = append(failures, commitSubjectFailures(args.Outputs.CommitSubject)...)

			result := qcResult{
				Passed:   len(failures) == 0,
				Failures: failures,
			}
			// Always emit a non-nil failures array for stable JSON shape.
			if result.Failures == nil {
				result.Failures = []qcFailure{}
			}

			out, marshalErr := json.MarshalIndent(result, "", "  ")
			if marshalErr != nil {
				return "", fmt.Errorf("marshal result: %w", marshalErr)
			}
			return string(out) + "\n", nil
		},
	}
}

// dryRunAmbiguityCheck inspects the bundle's mutations against the live
// resume.md content (read-only) and returns failures for any slug whose
// match count would prevent a deterministic apply.
//
// Rules per Phase 3b spec:
//   - thread_insert: slug must NOT already exist (>=1 match → fail).
//   - thread_replace: slug must match exactly once (0 or >1 matches → fail).
//   - thread_remove: same as replace.
//   - carried_add: slug must NOT already exist in carried-forward.
//   - carried_remove: slug must match exactly once.
func dryRunAmbiguityCheck(resumeContent string, bundle WrapBundle) []qcFailure {
	var failures []qcFailure

	threadCounts := countThreadSlugs(resumeContent)
	carriedCounts := countCarriedSlugs(resumeContent)

	for _, tb := range bundle.ResumeThreadBlocks {
		n := threadCounts[tb.Slug]
		switch {
		case n == 1:
			failures = append(failures, qcFailure{
				TriggerID: triggerMultiMatchAmbiguity,
				Detail:    fmt.Sprintf("thread_insert %q: slug already exists in resume.md", tb.Slug),
			})
		case n > 1:
			failures = append(failures, qcFailure{
				TriggerID: triggerMultiMatchAmbiguity,
				Detail:    fmt.Sprintf("thread_insert %q: %d matches found in resume.md (slug already ambiguous)", tb.Slug, n),
			})
		}
	}

	for _, tr := range bundle.ResumeThreadsReplace {
		n := threadCounts[tr.Slug]
		switch {
		case n == 0:
			failures = append(failures, qcFailure{
				TriggerID: triggerMultiMatchAmbiguity,
				Detail:    fmt.Sprintf("thread_replace %q: no anchor for replace operation", tr.Slug),
			})
		case n > 1:
			failures = append(failures, qcFailure{
				TriggerID: triggerMultiMatchAmbiguity,
				Detail:    fmt.Sprintf("thread_replace %q: %d matches found in resume.md", tr.Slug, n),
			})
		}
	}

	for _, tc := range bundle.ResumeThreadsToClose {
		n := threadCounts[tc.Slug]
		switch {
		case n == 0:
			failures = append(failures, qcFailure{
				TriggerID: triggerMultiMatchAmbiguity,
				Detail:    fmt.Sprintf("thread_remove %q: no anchor for remove operation", tc.Slug),
			})
		case n > 1:
			failures = append(failures, qcFailure{
				TriggerID: triggerMultiMatchAmbiguity,
				Detail:    fmt.Sprintf("thread_remove %q: %d matches found in resume.md", tc.Slug, n),
			})
		}
	}

	for _, ca := range bundle.CarriedChanges.Add {
		n := carriedCounts[strings.ToLower(ca.Slug)]
		if n >= 1 {
			failures = append(failures, qcFailure{
				TriggerID: triggerMultiMatchAmbiguity,
				Detail:    fmt.Sprintf("carried_add %q: slug already exists for insert (count=%d)", ca.Slug, n),
			})
		}
	}

	for _, cr := range bundle.CarriedChanges.Remove {
		n := carriedCounts[strings.ToLower(cr.Slug)]
		switch {
		case n == 0:
			failures = append(failures, qcFailure{
				TriggerID: triggerMultiMatchAmbiguity,
				Detail:    fmt.Sprintf("carried_remove %q: no anchor for remove operation", cr.Slug),
			})
		case n > 1:
			failures = append(failures, qcFailure{
				TriggerID: triggerMultiMatchAmbiguity,
				Detail:    fmt.Sprintf("carried_remove %q: %d matches found in carried-forward", cr.Slug, n),
			})
		}
	}

	return failures
}

// countThreadSlugs returns a map from normalized ### slug to the number of
// occurrences in the ## Open Threads section of resumeContent. The
// "Carried forward" sub-heading is not counted as a thread (it's owned by
// the carried-forward tools).
func countThreadSlugs(resumeContent string) map[string]int {
	counts := make(map[string]int)
	lines := strings.Split(resumeContent, "\n")

	parentStart := -1
	parentEnd := len(lines)
	for i, line := range lines {
		if strings.TrimSpace(line) == "## "+openThreadsSection {
			parentStart = i
			break
		}
	}
	if parentStart == -1 {
		return counts
	}
	for i := parentStart + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "## ") {
			parentEnd = i
			break
		}
	}
	for i := parentStart + 1; i < parentEnd; i++ {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "### ") {
			slug := mdutil.NormalizeSubheadingSlug(strings.TrimPrefix(line, "### "))
			if slug == carriedForwardSlug {
				continue
			}
			counts[slug]++
		}
	}
	return counts
}

// countCarriedSlugs returns a map from lowercased carried-forward slug to
// the number of occurrences inside the ### Carried forward sub-section.
// Returns an empty map when the section is missing.
func countCarriedSlugs(resumeContent string) map[string]int {
	counts := make(map[string]int)
	lines := strings.Split(resumeContent, "\n")

	parentStart := -1
	parentEnd := len(lines)
	for i, line := range lines {
		if strings.TrimSpace(line) == "## "+openThreadsSection {
			parentStart = i
			break
		}
	}
	if parentStart == -1 {
		return counts
	}
	for i := parentStart + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "## ") {
			parentEnd = i
			break
		}
	}
	cfStart := -1
	cfEnd := parentEnd
	for i := parentStart + 1; i < parentEnd; i++ {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "### ") {
			slug := mdutil.NormalizeSubheadingSlug(strings.TrimPrefix(line, "### "))
			if slug == carriedForwardSlug {
				cfStart = i
				for j := i + 1; j < parentEnd; j++ {
					if strings.HasPrefix(strings.TrimSpace(lines[j]), "### ") {
						cfEnd = j
						break
					}
				}
				break
			}
		}
	}
	if cfStart == -1 {
		return counts
	}
	body := strings.Join(lines[cfStart+1:cfEnd], "\n")
	for _, b := range mdutil.ParseCarriedForward(body) {
		counts[strings.ToLower(b.Slug)]++
	}
	return counts
}

// semanticPresenceFailures returns failures for the iteration narrative.
// A blank narrative is a single failure ("empty narrative"); otherwise we
// scan paragraph-by-paragraph and check the global commit-range invariant.
func semanticPresenceFailures(narrative string) []qcFailure {
	var failures []qcFailure
	trimmed := strings.TrimSpace(narrative)
	if trimmed == "" {
		failures = append(failures, qcFailure{
			TriggerID: triggerSemanticPresence,
			Detail:    "empty narrative",
		})
		return failures
	}

	paragraphs := splitParagraphs(trimmed)
	for i, para := range paragraphs {
		if !paragraphHasCitation(para) {
			failures = append(failures, qcFailure{
				TriggerID: triggerSemanticPresence,
				Detail:    fmt.Sprintf("paragraph %d has no citation", i+1),
			})
		}
	}

	if !qcRegexCommitRange.MatchString(trimmed) {
		failures = append(failures, qcFailure{
			TriggerID: triggerSemanticPresence,
			Detail:    "narrative missing commit range",
		})
	}
	return failures
}

// splitParagraphs splits text on blank-line separators, returning trimmed
// non-empty paragraphs. Single-newline breaks within a paragraph are
// preserved as a single paragraph (i.e., we only break on \n\n).
func splitParagraphs(text string) []string {
	// Normalise CRLF to LF before splitting.
	text = strings.ReplaceAll(text, "\r\n", "\n")
	parts := regexp.MustCompile(`\n\s*\n+`).Split(text, -1)
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// paragraphHasCitation returns true when at least one of the four
// recognized citation regexes matches anywhere in the paragraph.
func paragraphHasCitation(para string) bool {
	if qcRegexCommitSHA.MatchString(para) {
		return true
	}
	if qcRegexFilePath.MatchString(para) {
		return true
	}
	if qcRegexFuncName.MatchString(para) {
		return true
	}
	if qcRegexDecisionNum.MatchString(para) {
		return true
	}
	return false
}

// commitSubjectFailures returns the commit_subject_invalid failure(s).
// An empty subject and a subject in the rejection list each fire exactly
// once; we don't emit both because the rejection list is sampled only
// when the subject is non-empty.
func commitSubjectFailures(subject string) []qcFailure {
	if subject == "" {
		return []qcFailure{{
			TriggerID: triggerCommitSubjectInvalid,
			Detail:    "empty commit subject",
		}}
	}
	if rejectedCommitSubjects[subject] {
		return []qcFailure{{
			TriggerID: triggerCommitSubjectInvalid,
			Detail:    fmt.Sprintf("rejected lazy subject: %q", subject),
		}}
	}
	return nil
}

