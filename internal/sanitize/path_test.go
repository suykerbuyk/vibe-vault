// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package sanitize

import (
	"os"
	"testing"
)

func TestCompressHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("cannot get home dir: %v", err)
	}

	tests := []struct {
		name string
		path string
		want string
	}{
		{"under home", home + "/projects/foo", "~/projects/foo"},
		{"exactly home", home, "~"},
		{"not under home", "/var/log/syslog", "/var/log/syslog"},
		{"empty path", "", ""},
		{"home prefix but no slash", home + "extra", home + "extra"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CompressHome(tt.path)
			if got != tt.want {
				t.Errorf("CompressHome(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}
