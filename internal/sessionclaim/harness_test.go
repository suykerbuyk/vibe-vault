// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package sessionclaim

import (
	"errors"
	"testing"

	"github.com/suykerbuyk/vibe-vault/internal/pidlive"
)

func TestDetectHarness(t *testing.T) {
	cases := []struct {
		name       string
		parentName string
		parentErr  error
		want       string
	}{
		{"claude basename", "claude", nil, HarnessClaudeCode},
		{"claude-code basename", "claude-code", nil, HarnessClaudeCode},
		{"Claude Code app bundle", "Claude Code", nil, HarnessClaudeCode},
		{"Claude.app", "Claude.app", nil, HarnessClaudeCode},
		{"zed lowercase", "zed", nil, HarnessZedMCP},
		{"Zed app bundle", "Zed", nil, HarnessZedMCP},
		{"bash", "bash", nil, HarnessUnknown},
		{"empty", "", nil, HarnessUnknown},
		{"node", "node", nil, HarnessUnknown},
		{"err short-circuit", "claude", errors.New("boom"), HarnessUnknown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			orig := pidlive.ParentName
			parentName := c.parentName
			parentErr := c.parentErr
			pidlive.ParentName = func(int) (string, error) {
				return parentName, parentErr
			}
			t.Cleanup(func() { pidlive.ParentName = orig })

			got := DetectHarness(12345)
			if got != c.want {
				t.Errorf("DetectHarness(name=%q, err=%v) = %q, want %q",
					c.parentName, c.parentErr, got, c.want)
			}
		})
	}
}
