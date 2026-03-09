// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package zed

import (
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
	_ "modernc.org/sqlite"
)

// testRow represents a row to insert into the test DB.
type testRow struct {
	ID             string
	Summary        string
	UpdatedAt      string
	WorktreeBranch string
	ParentID       string
	Data           []byte // zstd-compressed JSON
}

// makeTestDB creates a temporary SQLite file with the threads table schema
// and inserts the given rows.
func makeTestDB(t *testing.T, rows ...testRow) string {
	t.Helper()
	path := t.TempDir() + "/threads.db"

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = db.Exec(`CREATE TABLE threads (
		id TEXT PRIMARY KEY,
		summary TEXT,
		updated_at TEXT,
		data_type TEXT DEFAULT 'zstd',
		data BLOB,
		parent_id TEXT,
		worktree_branch TEXT
	)`)
	if err != nil {
		t.Fatal(err)
	}

	for _, r := range rows {
		_, err = db.Exec(
			`INSERT INTO threads (id, summary, updated_at, worktree_branch, parent_id, data) VALUES (?, ?, ?, ?, ?, ?)`,
			r.ID, r.Summary, r.UpdatedAt, r.WorktreeBranch, r.ParentID, r.Data,
		)
		if err != nil {
			t.Fatal(err)
		}
	}

	return path
}

// compressJSON marshals v to JSON and compresses with zstd.
func compressJSON(t *testing.T, v interface{}) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	w, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	return w.EncodeAll(data, nil)
}

// compressJSONRaw compresses raw bytes with zstd.
func compressJSONRaw(t *testing.T, data []byte) []byte {
	t.Helper()
	w, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	return w.EncodeAll(data, nil)
}

// --- Raw JSON builders for Zed's Rust-style enum format ---

// rawThread builds a raw JSON map matching the Zed thread format.
type rawThread struct {
	Title             string                 `json:"title"`
	Model             *ZedModel              `json:"model"`
	Messages          []json.RawMessage      `json:"messages"`
	DetailedSummary   *string                `json:"detailed_summary"`
	ProjectSnapshot   *rawProjectSnapshot    `json:"initial_project_snapshot"`
	RequestTokenUsage map[string]TokenUsage  `json:"request_token_usage"`
	Version           string                 `json:"version"`
}

type rawProjectSnapshot struct {
	WorktreeSnapshots []WorktreeSnapshot `json:"worktree_snapshots"`
	Timestamp         string             `json:"timestamp"`
}

// threadOpt is a functional option for building test thread data.
type threadOpt func(*rawThread)

func withTitle(title string) threadOpt {
	return func(t *rawThread) { t.Title = title }
}

func withModel(provider, name string) threadOpt {
	return func(t *rawThread) {
		t.Model = &ZedModel{Provider: provider, Model: name}
	}
}

func withRawMessages(msgs ...json.RawMessage) threadOpt {
	return func(t *rawThread) { t.Messages = msgs }
}

func withDetailedSummary(s string) threadOpt {
	return func(t *rawThread) { t.DetailedSummary = &s }
}

func withSnapshot(worktreePath, branch, diff string) threadOpt {
	return func(t *rawThread) {
		t.ProjectSnapshot = &rawProjectSnapshot{
			WorktreeSnapshots: []WorktreeSnapshot{{
				WorktreePath: worktreePath,
				GitBranch:    branch,
				Diff:         diff,
			}},
		}
	}
}

func withRequestTokenUsage(usage map[string]TokenUsage) threadOpt {
	return func(t *rawThread) { t.RequestTokenUsage = usage }
}

// makeThreadJSON builds a raw JSON blob matching Zed's format.
func makeThreadJSON(t *testing.T, opts ...threadOpt) []byte {
	t.Helper()
	rt := rawThread{
		Title:   "Test Thread",
		Model:   &ZedModel{Provider: "anthropic", Model: "claude-sonnet-4-5-20250514"},
		Version: "0.3.0",
	}
	for _, opt := range opts {
		opt(&rt)
	}
	return compressJSON(t, rt)
}

// --- Raw message constructors matching Zed's {"User": {...}} / {"Agent": {...}} format ---

func rawUserMsg(t *testing.T, text string) json.RawMessage {
	t.Helper()
	msg := map[string]interface{}{
		"User": map[string]interface{}{
			"id":      "user-" + text[:min(8, len(text))],
			"content": []interface{}{map[string]string{"Text": text}},
		},
	}
	data, _ := json.Marshal(msg)
	return data
}

func rawUserMsgWithMention(t *testing.T, text, absPath string) json.RawMessage {
	t.Helper()
	msg := map[string]interface{}{
		"User": map[string]interface{}{
			"id": "user-mention",
			"content": []interface{}{
				map[string]string{"Text": text},
				map[string]interface{}{
					"Mention": map[string]interface{}{
						"uri":     map[string]interface{}{"File": map[string]string{"abs_path": absPath}},
						"content": "file contents here",
					},
				},
			},
		},
	}
	data, _ := json.Marshal(msg)
	return data
}

func rawAgentMsg(t *testing.T, text string) json.RawMessage {
	t.Helper()
	msg := map[string]interface{}{
		"Agent": map[string]interface{}{
			"content":      []interface{}{map[string]string{"Text": text}},
			"tool_results": map[string]interface{}{},
		},
	}
	data, _ := json.Marshal(msg)
	return data
}

func rawAgentMsgWithThinking(t *testing.T, thinking, text string) json.RawMessage {
	t.Helper()
	msg := map[string]interface{}{
		"Agent": map[string]interface{}{
			"content": []interface{}{
				map[string]interface{}{"Thinking": map[string]string{"text": thinking, "signature": "sig"}},
				map[string]string{"Text": text},
			},
			"tool_results": map[string]interface{}{},
		},
	}
	data, _ := json.Marshal(msg)
	return data
}

func rawAgentMsgWithTools(t *testing.T, text string, tools []interface{}, toolResults map[string]interface{}) json.RawMessage {
	t.Helper()
	content := []interface{}{}
	if text != "" {
		content = append(content, map[string]string{"Text": text})
	}
	content = append(content, tools...)
	msg := map[string]interface{}{
		"Agent": map[string]interface{}{
			"content":      content,
			"tool_results": toolResults,
		},
	}
	data, _ := json.Marshal(msg)
	return data
}

// rawToolUse creates a ToolUse content block in Zed's format.
func rawToolUse(name, id string, input map[string]interface{}) interface{} {
	return map[string]interface{}{
		"ToolUse": map[string]interface{}{
			"id":    id,
			"name":  name,
			"input": input,
		},
	}
}

// rawToolResult creates a tool result entry for the Agent's tool_results map.
func rawToolResult(toolUseID, toolName, output string, isError bool) interface{} {
	return map[string]interface{}{
		"tool_use_id": toolUseID,
		"tool_name":   toolName,
		"is_error":    isError,
		"content":     map[string]string{"Text": output},
		"output":      output,
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// testTime returns a fixed time for testing.
func testTime() time.Time {
	return time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)
}
