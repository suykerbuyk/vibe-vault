// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/suykerbuyk/vibe-vault/internal/config"
	"github.com/suykerbuyk/vibe-vault/internal/meta"
	"github.com/suykerbuyk/vibe-vault/internal/wrapbundlecache"
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

// BundleThreadReplace is a thread-body replacement entry ready for
// vv_thread_replace (H2-v3 in the wrap-model-tiering plan).
type BundleThreadReplace struct {
	Slug        string `json:"slug"`
	Body        string `json:"body"`
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

// WrapBundle is the top-level structure returned by the wrap-synthesis tool
// and consumed by the wrap-apply tool.
type WrapBundle struct {
	IterationBlock       BundleFieldWithContent `json:"iteration_block"`
	CommitMsg            BundleFieldWithContent `json:"commit_msg"`
	ResumeThreadBlocks   []BundleThreadBlock    `json:"resume_thread_blocks"`
	ResumeThreadsReplace []BundleThreadReplace  `json:"resume_threads_to_replace"`
	ResumeThreadsToClose []BundleThreadClose    `json:"resume_threads_to_close"`
	CarriedChanges       BundleCarriedChanges   `json:"carried_changes"`
	CaptureSession       BundleCaptureSession   `json:"capture_session"`
	SynthTimestamp       string                 `json:"synth_timestamp"`
	Iteration            int                    `json:"iteration"`
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

// SkeletonHandle is the {iter, skeleton_path, skeleton_sha256} reference
// passed by callers of vv_synthesize_wrap_bundle and
// vv_apply_wrap_bundle_by_handle.
type SkeletonHandle struct {
	Iter           int    `json:"iter"`
	SkeletonPath   string `json:"skeleton_path"`
	SkeletonSHA256 string `json:"skeleton_sha256"`
}

// loadSkeletonByHandle reads the skeleton file referenced by h, verifies its
// sha256 matches the handle (compare-and-set), and unmarshals into a
// WrapSkeleton. Returns an MCP-style error if the on-disk bytes have
// changed since the handle was issued.
func loadSkeletonByHandle(h SkeletonHandle) (WrapSkeleton, error) {
	if h.Iter <= 0 {
		return WrapSkeleton{}, fmt.Errorf("skeleton_handle.iter must be > 0")
	}
	if h.SkeletonPath == "" {
		return WrapSkeleton{}, fmt.Errorf("skeleton_handle.skeleton_path is required")
	}
	data, err := wrapbundlecache.Read(h.SkeletonPath)
	if err != nil {
		return WrapSkeleton{}, fmt.Errorf("read skeleton: %w", err)
	}
	got := sha256.Sum256(data)
	gotHex := hex.EncodeToString(got[:])
	if h.SkeletonSHA256 != "" && gotHex != h.SkeletonSHA256 {
		return WrapSkeleton{}, fmt.Errorf("skeleton cache file modified after handle issued (sha mismatch)")
	}
	var sk WrapSkeleton
	if err := json.Unmarshal(data, &sk); err != nil {
		return WrapSkeleton{}, fmt.Errorf("parse skeleton JSON: %w", err)
	}
	return sk, nil
}

// proseInputArgs is the shared input shape for prose fields used by both
// vv_synthesize_wrap_bundle and vv_apply_wrap_bundle_by_handle.
type proseInputArgs struct {
	IterationNarrative  string            `json:"iteration_narrative"`
	IterationTitle      string            `json:"iteration_title"`
	ProseBody           string            `json:"prose_body"`
	CommitSubject       string            `json:"commit_subject"`
	Date                string            `json:"date"`
	ThreadBodies        map[string]string `json:"thread_bodies"`
	CarriedBodies       map[string]string `json:"carried_bodies"`
	CaptureSummary      string            `json:"capture_summary"`
	CaptureTag          string            `json:"capture_tag"`
	CaptureDecisions    []string          `json:"capture_decisions"`
	CaptureFilesChanged []string          `json:"capture_files_changed"`
	CaptureOpenThreads  []string          `json:"capture_open_threads"`
}

// toProseFields translates the JSON input shape into the typed ProseFields
// consumed by FillBundle.
func (p proseInputArgs) toProseFields() ProseFields {
	return ProseFields(p)
}

// NewSynthesizeWrapTool creates the vv_synthesize_wrap_bundle tool.
//
// Loads a previously-prepared wrap skeleton from the host-local cache
// (referenced by skeleton_handle = {iter, skeleton_path, skeleton_sha256})
// and fills in the executor-supplied prose to produce a full WrapBundle.
//
// The bundle is NOT cached — each escalation tier's prose is ephemeral and
// regenerated from the same skeleton. The skeleton's sha256 is verified
// against the handle (compare-and-set) so a tampered cache file is detected.
func NewSynthesizeWrapTool(_ config.Config) Tool {
	return Tool{
		Definition: ToolDef{
			Name: "vv_synthesize_wrap_bundle",
			Description: "Reconstruct a full WrapBundle from a previously-prepared " +
				"skeleton handle plus executor-supplied prose. The skeleton handle is " +
				"the {iter, skeleton_path, skeleton_sha256} object returned by " +
				"vv_prepare_wrap_skeleton; on-disk bytes are sha256-verified against " +
				"the handle to detect cache tampering. The returned bundle is in-memory " +
				"and ephemeral — each escalation tier regenerates prose from the same " +
				"skeleton. Pass the bundle (unchanged) to vv_apply_wrap_bundle_by_handle " +
				"to dispatch all writes.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"skeleton_handle": {
						"type": "object",
						"description": "The {iter, skeleton_path, skeleton_sha256} object returned by vv_prepare_wrap_skeleton.",
						"properties": {
							"iter":            {"type": "integer"},
							"skeleton_path":   {"type": "string"},
							"skeleton_sha256": {"type": "string"}
						},
						"required": ["iter", "skeleton_path"]
					},
					"iteration_narrative":   {"type": "string"},
					"iteration_title":       {"type": "string"},
					"prose_body":            {"type": "string"},
					"commit_subject":        {"type": "string"},
					"date":                  {"type": "string"},
					"thread_bodies":         {"type": "object", "description": "Map of slug -> body for opened + replaced threads."},
					"carried_bodies":        {"type": "object", "description": "Map of slug -> body for added carried items."},
					"capture_summary":       {"type": "string"},
					"capture_tag":           {"type": "string"},
					"capture_decisions":     {"type": "array", "items": {"type": "string"}},
					"capture_files_changed": {"type": "array", "items": {"type": "string"}},
					"capture_open_threads":  {"type": "array", "items": {"type": "string"}}
				},
				"required": ["skeleton_handle"]
			}`),
		},
		Handler: func(params json.RawMessage) (string, error) {
			var args struct {
				SkeletonHandle SkeletonHandle `json:"skeleton_handle"`
				proseInputArgs
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
			if strings.Contains(args.CommitSubject, "\n") {
				return "", fmt.Errorf("commit_subject must be a single line (no newlines)")
			}
			bundle := FillBundle(skeleton, args.toProseFields())
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

