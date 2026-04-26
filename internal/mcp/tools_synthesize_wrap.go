// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/suykerbuyk/vibe-vault/internal/config"
	"github.com/suykerbuyk/vibe-vault/internal/meta"
)

// ---- Bundle types -----------------------------------------------------------

// BundleFieldWithContent holds pre-formatted content and its synthesize-time
// SHA-256 fingerprint.
type BundleFieldWithContent struct {
	Content     string `json:"content"`
	SynthSHA256 string `json:"synth_sha256"`
}

// BundleThreadBlock is a single resume thread entry ready for vv_thread_insert.
type BundleThreadBlock struct {
	Position    map[string]string `json:"position"`
	Slug        string            `json:"slug"`
	Body        string            `json:"body"`
	SynthSHA256 string            `json:"synth_sha256"`
}

// BundleThreadClose is a thread removal entry ready for vv_thread_remove.
type BundleThreadClose struct {
	Slug        string `json:"slug"`
	SynthSHA256 string `json:"synth_sha256"`
}

// BundleCarriedAdd is a single carried-forward add entry ready for vv_carried_add.
type BundleCarriedAdd struct {
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	Body        string `json:"body"`
	SynthSHA256 string `json:"synth_sha256"`
}

// BundleCarriedRemove is a carried-forward remove entry ready for vv_carried_remove.
type BundleCarriedRemove struct {
	Slug        string `json:"slug"`
	SynthSHA256 string `json:"synth_sha256"`
}

// BundleCarriedChanges holds the add and remove lists.
type BundleCarriedChanges struct {
	Add    []BundleCarriedAdd    `json:"add"`
	Remove []BundleCarriedRemove `json:"remove"`
}

// BundleCaptureSession holds the capture_session payload and its fingerprint.
type BundleCaptureSession struct {
	Content     BundleCaptureContent `json:"content"`
	SynthSHA256 string               `json:"synth_sha256"`
}

// BundleCaptureContent mirrors the inputs to vv_capture_session.
type BundleCaptureContent struct {
	Summary      string   `json:"summary"`
	Tag          string   `json:"tag"`
	Decisions    []string `json:"decisions"`
	FilesChanged []string `json:"files_changed"`
	OpenThreads  []string `json:"open_threads"`
}

// WrapBundle is the top-level structure returned by vv_synthesize_wrap and
// consumed by vv_apply_wrap_bundle.
type WrapBundle struct {
	IterationBlock      BundleFieldWithContent `json:"iteration_block"`
	CommitMsg           BundleFieldWithContent `json:"commit_msg"`
	ResumeThreadBlocks  []BundleThreadBlock    `json:"resume_thread_blocks"`
	ResumeThreadsToClose []BundleThreadClose   `json:"resume_threads_to_close"`
	CarriedChanges      BundleCarriedChanges   `json:"carried_changes"`
	CaptureSession      BundleCaptureSession   `json:"capture_session"`
	SynthTimestamp      string                 `json:"synth_timestamp"`
	Iteration           int                    `json:"iteration"`
}

// fingerprintString returns the hex-encoded SHA-256 of s.
func fingerprintString(s string) string {
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", h)
}

// fingerprintJSON returns the hex-encoded SHA-256 of the JSON-marshalled v.
func fingerprintJSON(v any) (string, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h), nil
}

// NewSynthesizeWrapTool creates the vv_synthesize_wrap tool.
//
// The tool assembles all wrap sub-artifacts into a JSON bundle — iteration
// block, commit message, thread edits, carried-forward changes, and a
// capture_session payload — each tagged with a synthesize-time SHA-256
// fingerprint. It does NOT write any file; the AI inspects and optionally
// edits the bundle before passing it to vv_apply_wrap_bundle.
func NewSynthesizeWrapTool(cfg config.Config) Tool {
	return Tool{
		Definition: ToolDef{
			Name: "vv_synthesize_wrap",
			Description: "Assemble all wrap sub-artifacts (iteration_block, commit_msg, " +
				"resume_thread_blocks, carried_changes, capture_session) into a single " +
				"JSON bundle, each field tagged with a synthesize-time SHA-256 fingerprint. " +
				"Does NOT write any file — pass the returned bundle to vv_apply_wrap_bundle " +
				"to dispatch all writes atomically. " +
				"iteration_narrative and title are required; subject is required for the " +
				"commit message. If files_changed or test_count_delta are omitted, the " +
				"files section is derived from git and test counts default to zero.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"project": {
						"type": "string",
						"description": "Project name. If omitted, detected from working directory."
					},
					"project_path": {
						"type": "string",
						"description": "Absolute path to the project root (for git-derived sections). If omitted, derived via meta.ProjectRoot()."
					},
					"iteration": {
						"type": "integer",
						"description": "Iteration number. If omitted, defaults to 0 (apply tool may auto-increment)."
					},
					"iteration_narrative": {
						"type": "string",
						"description": "Required. Verbatim narrative prose for iterations.md."
					},
					"title": {
						"type": "string",
						"description": "Required. Short title for the iteration heading."
					},
					"subject": {
						"type": "string",
						"description": "Required. Single-line commit subject (first line of commit message)."
					},
					"prose_body": {
						"type": "string",
						"description": "2-3 paragraph commit narrative. Defaults to iteration_narrative if omitted."
					},
					"date": {
						"type": "string",
						"description": "Date in YYYY-MM-DD format. Defaults to today."
					},
					"files_changed": {
						"type": "array",
						"items": {"type": "string"},
						"description": "Files changed. If omitted, derived from git status/diff in project root."
					},
					"test_count_delta": {
						"type": "object",
						"description": "Test counts for the commit message.",
						"properties": {
							"unit_tests": {"type": "integer"},
							"integration_subtests": {"type": "integer"},
							"lint_findings": {"type": "integer"}
						}
					},
					"decisions": {
						"type": "array",
						"items": {"type": "string"},
						"description": "Key decisions for capture_session."
					},
					"threads_to_open": {
						"type": "array",
						"description": "New threads to insert. Each item: {position: {mode, anchor_slug?}, slug, body}.",
						"items": {
							"type": "object",
							"properties": {
								"position": {"type": "object"},
								"slug": {"type": "string"},
								"body": {"type": "string"}
							},
							"required": ["position", "slug", "body"]
						}
					},
					"threads_to_close": {
						"type": "array",
						"items": {"type": "string"},
						"description": "Slugs of threads to remove."
					},
					"carried_to_add": {
						"type": "array",
						"description": "Carried-forward bullets to add. Each: {slug, title, body?}.",
						"items": {
							"type": "object",
							"properties": {
								"slug": {"type": "string"},
								"title": {"type": "string"},
								"body": {"type": "string"}
							},
							"required": ["slug", "title"]
						}
					},
					"carried_to_remove": {
						"type": "array",
						"items": {"type": "string"},
						"description": "Slugs of carried-forward bullets to remove."
					}
				},
				"required": ["iteration_narrative", "title", "subject"]
			}`),
		},
		Handler: func(params json.RawMessage) (string, error) {
			var args struct {
				Project            string   `json:"project"`
				ProjectPath        string   `json:"project_path"`
				Iteration          int      `json:"iteration"`
				IterationNarrative string   `json:"iteration_narrative"`
				Title              string   `json:"title"`
				Subject            string   `json:"subject"`
				ProseBody          string   `json:"prose_body"`
				Date               string   `json:"date"`
				FilesChanged       []string `json:"files_changed"`
				TestCountDelta     *struct {
					UnitTests           int `json:"unit_tests"`
					IntegrationSubtests int `json:"integration_subtests"`
					LintFindings        int `json:"lint_findings"`
				} `json:"test_count_delta"`
				Decisions       []string `json:"decisions"`
				ThreadsToOpen   []struct {
					Position map[string]string `json:"position"`
					Slug     string            `json:"slug"`
					Body     string            `json:"body"`
				} `json:"threads_to_open"`
				ThreadsToClose  []string `json:"threads_to_close"`
				CarriedToAdd    []struct {
					Slug  string `json:"slug"`
					Title string `json:"title"`
					Body  string `json:"body"`
				} `json:"carried_to_add"`
				CarriedToRemove []string `json:"carried_to_remove"`
			}
			if len(params) > 0 {
				if err := json.Unmarshal(params, &args); err != nil {
					return "", fmt.Errorf("invalid arguments: %w", err)
				}
			}

			// Validate required fields.
			if args.IterationNarrative == "" {
				return "", fmt.Errorf("iteration_narrative is required")
			}
			if args.Title == "" {
				return "", fmt.Errorf("title is required")
			}
			if args.Subject == "" {
				return "", fmt.Errorf("subject is required")
			}
			if strings.Contains(args.Subject, "\n") {
				return "", fmt.Errorf("subject must be a single line (no newlines)")
			}

			// Default date.
			date := args.Date
			if date == "" {
				date = time.Now().Format("2006-01-02")
			}

			// Default prose_body to iteration_narrative if not supplied.
			proseBody := args.ProseBody
			if proseBody == "" {
				proseBody = args.IterationNarrative
			}

			// --- Build iteration_block ---
			iterBlock := BuildIterationBlock(args.Iteration, args.Title, args.IterationNarrative, date, cfg.VaultPath)

			// --- Build commit_msg ---
			// Resolve project root for git-derived sections.
			projectRoot, rootErr := resolveProjectRoot(args.ProjectPath, cfg.VaultPath)
			var filesSection string
			if len(args.FilesChanged) > 0 {
				// Build from supplied list.
				var sb strings.Builder
				for _, f := range args.FilesChanged {
					sb.WriteString("- ")
					sb.WriteString(f)
					sb.WriteString("\n")
				}
				filesSection = sb.String()
			} else if rootErr == nil {
				// Derive from git in project root.
				var err error
				filesSection, err = buildFilesChangedSection(projectRoot)
				if err != nil {
					// Non-fatal: fall back to placeholder.
					filesSection = "(could not derive files changed: " + err.Error() + ")\n"
				}
			} else {
				filesSection = "(project root not resolved — pass project_path for git-derived section)\n"
			}

			unitTests, integrationSubtests, lintFindings := 0, 0, 0
			if args.TestCountDelta != nil {
				unitTests = args.TestCountDelta.UnitTests
				integrationSubtests = args.TestCountDelta.IntegrationSubtests
				lintFindings = args.TestCountDelta.LintFindings
			}

			commitMsgContent := renderCommitMsg(
				args.Subject,
				proseBody,
				filesSection,
				unitTests,
				integrationSubtests,
				lintFindings,
				args.Iteration,
			)

			// --- Build resume_thread_blocks ---
			threadBlocks := make([]BundleThreadBlock, 0, len(args.ThreadsToOpen))
			for _, t := range args.ThreadsToOpen {
				fp := fingerprintString(t.Slug + "\x00" + t.Body)
				threadBlocks = append(threadBlocks, BundleThreadBlock{
					Position:    t.Position,
					Slug:        t.Slug,
					Body:        t.Body,
					SynthSHA256: fp,
				})
			}

			// --- Build resume_threads_to_close ---
			threadClose := make([]BundleThreadClose, 0, len(args.ThreadsToClose))
			for _, slug := range args.ThreadsToClose {
				threadClose = append(threadClose, BundleThreadClose{
					Slug:        slug,
					SynthSHA256: fingerprintString(slug),
				})
			}

			// --- Build carried_changes ---
			carriedAdd := make([]BundleCarriedAdd, 0, len(args.CarriedToAdd))
			for _, ca := range args.CarriedToAdd {
				fp := fingerprintString(ca.Slug + "\x00" + ca.Title + "\x00" + ca.Body)
				carriedAdd = append(carriedAdd, BundleCarriedAdd{
					Slug:        ca.Slug,
					Title:       ca.Title,
					Body:        ca.Body,
					SynthSHA256: fp,
				})
			}
			carriedRemove := make([]BundleCarriedRemove, 0, len(args.CarriedToRemove))
			for _, slug := range args.CarriedToRemove {
				carriedRemove = append(carriedRemove, BundleCarriedRemove{
					Slug:        slug,
					SynthSHA256: fingerprintString(slug),
				})
			}

			// --- Build capture_session ---
			// open_threads derived from threads_to_open slugs + threads_to_close slugs.
			openThreads := make([]string, 0)
			for _, t := range args.ThreadsToOpen {
				openThreads = append(openThreads, t.Slug)
			}
			for _, slug := range args.ThreadsToClose {
				openThreads = append(openThreads, "close:"+slug)
			}

			filesForCapture := args.FilesChanged
			if len(filesForCapture) == 0 {
				// Derive from the filesSection we already have (parse bullet lines).
				for _, line := range strings.Split(filesSection, "\n") {
					if strings.HasPrefix(line, "- ") {
						filesForCapture = append(filesForCapture, strings.TrimPrefix(line, "- "))
					}
				}
			}

			captureContent := BundleCaptureContent{
				Summary:      args.Title + ". " + firstNWords(args.IterationNarrative, 30),
				Tag:          "implementation",
				Decisions:    args.Decisions,
				FilesChanged: filesForCapture,
				OpenThreads:  openThreads,
			}
			captureFP, fpErr := fingerprintJSON(captureContent)
			if fpErr != nil {
				captureFP = "sha256-error:" + fpErr.Error()
			}

			// --- Assemble the bundle ---
			bundle := WrapBundle{
				IterationBlock: BundleFieldWithContent{
					Content:     iterBlock,
					SynthSHA256: fingerprintString(iterBlock),
				},
				CommitMsg: BundleFieldWithContent{
					Content:     commitMsgContent,
					SynthSHA256: fingerprintString(commitMsgContent),
				},
				ResumeThreadBlocks:   threadBlocks,
				ResumeThreadsToClose: threadClose,
				CarriedChanges: BundleCarriedChanges{
					Add:    carriedAdd,
					Remove: carriedRemove,
				},
				CaptureSession: BundleCaptureSession{
					Content:     captureContent,
					SynthSHA256: captureFP,
				},
				SynthTimestamp: time.Now().UTC().Format(time.RFC3339),
				Iteration:      args.Iteration,
			}

			out, err := json.MarshalIndent(bundle, "", "  ")
			if err != nil {
				return "", fmt.Errorf("marshal bundle: %w", err)
			}
			return string(out) + "\n", nil
		},
	}
}

// firstNWords returns the first n words of s, joining with spaces. Used to
// produce a short summary for the capture_session payload.
func firstNWords(s string, n int) string {
	words := strings.Fields(s)
	if len(words) > n {
		words = words[:n]
	}
	return strings.Join(words, " ")
}

// provenance helpers used by apply tool.
func provenanceForMetrics() (host, user, cwd string) {
	p := meta.Stamp()
	return p.Host, p.User, p.CWD
}
