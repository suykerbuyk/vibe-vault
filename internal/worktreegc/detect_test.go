// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package worktreegc

import "testing"

func TestParseMarker_Claude_TrimsAndMatches(t *testing.T) {
	harness, pid, det := ParseMarker("claude agent agent-aaaaaaaaaaaaaaaa (pid 1234)\n", nil)
	if harness != "claude" {
		t.Errorf("harness = %q, want claude", harness)
	}
	if pid != 1234 {
		t.Errorf("pid = %d, want 1234", pid)
	}
	if det == nil {
		t.Error("detector is nil, want non-nil")
	}
}

func TestParseMarker_Claude_AnchorRejectsNoise(t *testing.T) {
	harness, _, det := ParseMarker("garbage claude agent agent-aaaaaaaaaaaaaaaa (pid 1234) noise", nil)
	if harness != "" {
		t.Errorf("harness = %q, want empty (anchored regex must reject noise)", harness)
	}
	if det != nil {
		t.Errorf("detector = %v, want nil", det)
	}
}

func TestParseMarker_Claude_RejectsShortID(t *testing.T) {
	harness, _, det := ParseMarker("claude agent agent-aaaaaaaa (pid 1234)", nil)
	if harness != "" {
		t.Errorf("harness = %q, want empty (8 hex chars must be rejected)", harness)
	}
	if det != nil {
		t.Errorf("detector = %v, want nil", det)
	}
}

func TestParseMarker_Unknown(t *testing.T) {
	harness, pid, det := ParseMarker("some random reason", nil)
	if harness != "" {
		t.Errorf("harness = %q, want empty", harness)
	}
	if pid != 0 {
		t.Errorf("pid = %d, want 0", pid)
	}
	if det != nil {
		t.Errorf("detector = %v, want nil", det)
	}
}
