// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package zed

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/klauspost/compress/zstd"
	_ "modernc.org/sqlite" // pure-Go SQLite driver
)

// DefaultDBPath returns the default Zed threads database path.
func DefaultDBPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "share", "zed", "threads", "threads.db")
}

// ParseOpts controls thread filtering.
type ParseOpts struct {
	Since time.Time // Only threads updated after this time.
	Limit int       // Max threads to return (0 = unlimited).
}

// ParseDB opens a Zed threads database and returns parsed threads.
func ParseDB(dbPath string, opts ParseOpts) ([]Thread, error) {
	db, err := sql.Open("sqlite", dbPath+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	query := `SELECT id, COALESCE(summary, ''), COALESCE(updated_at, ''), COALESCE(worktree_branch, ''), COALESCE(parent_id, ''), data FROM threads`
	var args []interface{}

	if !opts.Since.IsZero() {
		query += ` WHERE updated_at >= ?`
		args = append(args, opts.Since.Format(time.RFC3339))
	}

	query += ` ORDER BY updated_at DESC`

	if opts.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, opts.Limit)
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query threads: %w", err)
	}
	defer rows.Close()

	var threads []Thread
	for rows.Next() {
		var id, summary, updatedAt, branch, parentID string
		var data []byte
		if err := rows.Scan(&id, &summary, &updatedAt, &branch, &parentID, &data); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}

		t, err := ParseThread(id, summary, updatedAt, branch, parentID, data)
		if err != nil {
			log.Printf("warning: skipping thread %s: %v", id, err)
			continue
		}
		threads = append(threads, *t)
	}

	return threads, rows.Err()
}

// ParseThread decompresses and unmarshals a single thread's data blob.
func ParseThread(id, summary, updatedAt, worktreeBranch, parentID string, data []byte) (*Thread, error) {
	decoded, err := decompressZstd(data)
	if err != nil {
		return nil, fmt.Errorf("decompress: %w", err)
	}

	var t Thread
	if err = json.Unmarshal(decoded, &t); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}

	// Parse Rust-style enum messages from RawMessages.
	messages, err := UnmarshalMessages(t.RawMessages)
	if err != nil {
		return nil, fmt.Errorf("parse messages: %w", err)
	}
	t.Messages = messages
	t.RawMessages = nil // free memory

	t.ID = id
	t.Summary = summary
	t.WorktreeBranch = worktreeBranch
	t.ParentID = parentID

	if updatedAt != "" {
		if parsed, err := time.Parse(time.RFC3339, updatedAt); err == nil {
			t.UpdatedAt = parsed
		} else {
			// Try RFC3339Nano (Zed uses nanosecond precision)
			if parsed, err := time.Parse(time.RFC3339Nano, updatedAt); err == nil {
				t.UpdatedAt = parsed
			}
		}
	}

	if t.Version != "" && t.Version != "0.3.0" {
		log.Printf("warning: thread %s has version %s, parser tested with 0.3.0", id, t.Version)
	}

	return &t, nil
}

// QueryThread opens a Zed threads database and returns a single parsed thread by ID.
func QueryThread(dbPath, threadID string) (*Thread, error) {
	db, err := sql.Open("sqlite", dbPath+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	var id, summary, updatedAt, branch, parentID string
	var data []byte
	err = db.QueryRow(
		`SELECT id, COALESCE(summary, ''), COALESCE(updated_at, ''), COALESCE(worktree_branch, ''), COALESCE(parent_id, ''), data FROM threads WHERE id = ?`,
		threadID,
	).Scan(&id, &summary, &updatedAt, &branch, &parentID, &data)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("thread %q not found", threadID)
		}
		return nil, fmt.Errorf("query thread: %w", err)
	}

	return ParseThread(id, summary, updatedAt, branch, parentID, data)
}

// decompressZstd decompresses zstd-compressed data.
func decompressZstd(data []byte) ([]byte, error) {
	r, err := zstd.NewReader(nil)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return r.DecodeAll(data, nil)
}
