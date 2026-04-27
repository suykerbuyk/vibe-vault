// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"encoding/json"
	"fmt"

	"github.com/suykerbuyk/vibe-vault/internal/wrapbundlecache"
)

// NewPrepareWrapSkeletonTool creates the vv_prepare_wrap_skeleton tool.
//
// The tool persists orchestrator-collected facts (skeleton) for a wrap
// iteration to host-local cache so subsequent escalation tiers can reuse
// the same facts without re-collecting them. Each tier's prose is later
// filled from the skeleton via vv_synthesize_wrap_bundle (ephemeral, not
// cached).
//
// Returns {iter, skeleton_path, skeleton_sha256}. After writing, the cache
// is rotated to keep only the 3 most recent skeletons (Decision 12).
func NewPrepareWrapSkeletonTool() Tool {
	return Tool{
		Definition: ToolDef{
			Name: "vv_prepare_wrap_skeleton",
			Description: "Persist orchestrator-collected wrap facts (iter, project, files " +
				"changed, decisions, edit plan as slug lists, task retirements) to a host-" +
				"local cache file. Returns a {iter, skeleton_path, skeleton_sha256} handle " +
				"that subsequent vv_synthesize_wrap_bundle / vv_apply_wrap_bundle_by_handle " +
				"calls reference. The skeleton carries NO prose; bodies are supplied later " +
				"per escalation tier. The cache is rotated to keep the 3 most recent " +
				"skeletons.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"iter":             {"type": "integer", "description": "Iteration number (required, > 0)."},
					"project":          {"type": "string",  "description": "Project name (required)."},
					"files_changed":    {"type": "array", "items": {"type": "string"}, "description": "Files touched in this iteration."},
					"test_count_delta": {"type": "integer", "description": "Net test count change (simple int sum)."},
					"decisions":        {"type": "array", "items": {"type": "string"}, "description": "Key decisions for capture_session."},
					"threads_to_open": {
						"type": "array",
						"description": "Threads to open. Each: {slug, anchor_before?, anchor_after?}. NO body.",
						"items": {
							"type": "object",
							"properties": {
								"slug":          {"type": "string"},
								"anchor_before": {"type": "string"},
								"anchor_after":  {"type": "string"}
							},
							"required": ["slug"]
						}
					},
					"threads_to_replace": {
						"type": "array",
						"description": "Threads whose body will be replaced (H2-v3). Each: {slug}. NO body.",
						"items": {
							"type": "object",
							"properties": {"slug": {"type": "string"}},
							"required": ["slug"]
						}
					},
					"threads_to_close": {
						"type": "array",
						"description": "Threads to remove. Each: {slug}.",
						"items": {
							"type": "object",
							"properties": {"slug": {"type": "string"}},
							"required": ["slug"]
						}
					},
					"carried_to_add": {
						"type": "array",
						"description": "Carried-forward bullets to add. Each: {slug, title}. NO body.",
						"items": {
							"type": "object",
							"properties": {
								"slug":  {"type": "string"},
								"title": {"type": "string"}
							},
							"required": ["slug", "title"]
						}
					},
					"carried_to_remove": {
						"type": "array",
						"description": "Carried-forward bullets to remove. Each: {slug}.",
						"items": {
							"type": "object",
							"properties": {"slug": {"type": "string"}},
							"required": ["slug"]
						}
					},
					"task_retirements": {
						"type": "array",
						"description": "Tasks to mark retired. Each: {task, note?}.",
						"items": {
							"type": "object",
							"properties": {
								"task": {"type": "string"},
								"note": {"type": "string"}
							},
							"required": ["task"]
						}
					}
				},
				"required": ["iter", "project"]
			}`),
		},
		Handler: func(params json.RawMessage) (string, error) {
			var args struct {
				Iter             int                       `json:"iter"`
				Project          string                    `json:"project"`
				FilesChanged     []string                  `json:"files_changed"`
				TestCountDelta   int                       `json:"test_count_delta"`
				Decisions        []string                  `json:"decisions"`
				ThreadsToOpen    []SkeletonThreadOpen      `json:"threads_to_open"`
				ThreadsToReplace []SkeletonThreadReplace   `json:"threads_to_replace"`
				ThreadsToClose   []SkeletonThreadClose     `json:"threads_to_close"`
				CarriedToAdd     []SkeletonCarriedAdd      `json:"carried_to_add"`
				CarriedToRemove  []SkeletonCarriedRemove   `json:"carried_to_remove"`
				TaskRetirements  []SkeletonTaskRetirement  `json:"task_retirements"`
			}
			if len(params) > 0 {
				if err := json.Unmarshal(params, &args); err != nil {
					return "", fmt.Errorf("invalid arguments: %w", err)
				}
			}
			if args.Iter <= 0 {
				return "", fmt.Errorf("iter is required and must be > 0")
			}
			if args.Project == "" {
				return "", fmt.Errorf("project is required")
			}

			facts := SkeletonFacts{
				Iter:                 args.Iter,
				Project:              args.Project,
				FilesChanged:         args.FilesChanged,
				TestCountDelta:       args.TestCountDelta,
				Decisions:            args.Decisions,
				ResumeThreadBlocks:   args.ThreadsToOpen,
				ResumeThreadsReplace: args.ThreadsToReplace,
				ResumeThreadsToClose: args.ThreadsToClose,
				CarriedChangesAdd:    args.CarriedToAdd,
				CarriedChangesRemove: args.CarriedToRemove,
				TaskRetirements:      args.TaskRetirements,
			}
			skeleton := BuildSkeleton(facts)

			data, err := json.MarshalIndent(skeleton, "", "  ")
			if err != nil {
				return "", fmt.Errorf("marshal skeleton: %w", err)
			}
			path, sha, err := wrapbundlecache.Write(args.Iter, data)
			if err != nil {
				return "", fmt.Errorf("write skeleton cache: %w", err)
			}
			// Best-effort rotation: keep the 3 most recent skeletons (Decision 12).
			// Rotation errors are non-fatal — the write already succeeded.
			_, _ = wrapbundlecache.RotateKeepN(3)

			out := struct {
				Iter            int    `json:"iter"`
				SkeletonPath    string `json:"skeleton_path"`
				SkeletonSHA256  string `json:"skeleton_sha256"`
			}{
				Iter:           args.Iter,
				SkeletonPath:   path,
				SkeletonSHA256: sha,
			}
			b, marshalErr := json.MarshalIndent(out, "", "  ")
			if marshalErr != nil {
				return "", fmt.Errorf("marshal response: %w", marshalErr)
			}
			return string(b) + "\n", nil
		},
	}
}
