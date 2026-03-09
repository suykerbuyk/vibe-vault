// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package zed

import "testing"

func TestQueryThread_Found(t *testing.T) {
	data := makeThreadJSON(t,
		withTitle("Query Test"),
		withRawMessages(
			rawUserMsg(t, "Hello"),
			rawAgentMsg(t, "Hi there"),
		),
	)

	dbPath := makeTestDB(t,
		testRow{ID: "thread-abc", Summary: "Test summary", UpdatedAt: "2026-03-08T12:00:00Z", Data: data},
		testRow{ID: "thread-def", Summary: "Other", UpdatedAt: "2026-03-07T12:00:00Z", Data: data},
	)

	thread, err := QueryThread(dbPath, "thread-abc")
	if err != nil {
		t.Fatal(err)
	}
	if thread.ID != "thread-abc" {
		t.Errorf("ID = %q, want %q", thread.ID, "thread-abc")
	}
	if thread.Title != "Query Test" {
		t.Errorf("Title = %q, want %q", thread.Title, "Query Test")
	}
	if thread.Summary != "Test summary" {
		t.Errorf("Summary = %q, want %q", thread.Summary, "Test summary")
	}
	if len(thread.Messages) != 2 {
		t.Errorf("Messages = %d, want 2", len(thread.Messages))
	}
}

func TestQueryThread_NotFound(t *testing.T) {
	data := makeThreadJSON(t)
	dbPath := makeTestDB(t,
		testRow{ID: "thread-abc", Summary: "Test", UpdatedAt: "2026-03-08T12:00:00Z", Data: data},
	)

	_, err := QueryThread(dbPath, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent thread")
	}
}

func TestQueryThread_BadDB(t *testing.T) {
	_, err := QueryThread("/nonexistent/threads.db", "any-id")
	if err == nil {
		t.Error("expected error for nonexistent DB")
	}
}
