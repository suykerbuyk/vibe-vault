// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package staging

import (
	"path/filepath"
	"time"

	"github.com/suykerbuyk/vibe-vault/internal/render"
)

// NotePath returns the absolute path for a session note inside the
// host-local staging dir: <root>/<project>/<filename> where filename is
// the canonical timestamp form produced by render.BuildTimestampFilename.
//
// No host segment — the staging dir is per-host (only this host's
// processes ever touch it). The wrap-time mirror in Phase 3 is what
// adds the per-host segment when projecting staging contents into the
// shared vault.
//
// No I/O. Pure path composition; callers handle MkdirAll on the
// returned path's parent before writing.
//
// suffix follows render.BuildTimestampFilename semantics: 0 → no
// trailing -N, 1..9 → "-N" collision-retry suffix.
func NotePath(root, project, date string, t time.Time, suffix int) string {
	filename := render.BuildTimestampFilename(date, t, suffix)
	return filepath.Join(root, project, filename)
}
