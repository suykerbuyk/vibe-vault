// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package main

import "testing"

func TestParseZedTranscriptPath(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		wantDB string
		wantID string
		wantOK bool
	}{
		{
			"valid path",
			"zed:/home/user/.local/share/zed/threads/threads.db#abc-123",
			"/home/user/.local/share/zed/threads/threads.db",
			"abc-123",
			true,
		},
		{
			"without zed: prefix",
			"/path/to/db#thread-id",
			"/path/to/db",
			"thread-id",
			true,
		},
		{
			"no hash separator",
			"zed:/path/to/db",
			"", "", false,
		},
		{
			"trailing hash only",
			"zed:/path/to/db#",
			"", "", false,
		},
		{
			"multiple hashes uses last",
			"zed:/path#to/db#thread-id",
			"/path#to/db",
			"thread-id",
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dbPath, threadID, ok := parseZedTranscriptPath(tt.input)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if dbPath != tt.wantDB {
				t.Errorf("dbPath = %q, want %q", dbPath, tt.wantDB)
			}
			if threadID != tt.wantID {
				t.Errorf("threadID = %q, want %q", threadID, tt.wantID)
			}
		})
	}
}
