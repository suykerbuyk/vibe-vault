// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package sanitize

import (
	"os"
	"strings"
)

// CompressHome replaces $HOME prefix with ~/ for portable path values.
func CompressHome(path string) string {
	// NOTE: stays on stdlib os.UserHomeDir intentionally — internal/meta
	// imports internal/sanitize (meta/sanitize.go), so a meta.HomeDir()
	// call here would create an import cycle. Document, don't migrate.
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if strings.HasPrefix(path, home+"/") {
		return "~/" + path[len(home)+1:]
	}
	if path == home {
		return "~"
	}
	return path
}
