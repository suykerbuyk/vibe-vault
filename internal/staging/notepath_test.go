// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package staging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// osStat is an alias kept readable inline for the I/O-free assertion.
var osStat = os.Stat

// TestNotePath_FilenameMatchesRender locks the contract that
// NotePath uses render.BuildTimestampFilename for the filename
// portion. A divergence here would mean staging filenames stop
// sorting alongside vault filenames in chronological order — the
// Mechanism 1 invariant the timestamp format encodes.
func TestNotePath_FilenameMatchesRender(t *testing.T) {
	root := "/staging-root"
	project := "demo"
	when := time.Date(2026, 5, 3, 14, 30, 25, 123_000_000, time.UTC)
	date := when.Format("2006-01-02")

	got := NotePath(root, project, date, when, 0)
	want := filepath.Join(root, project, "2026-05-03-143025123.md")
	if got != want {
		t.Errorf("NotePath suffix=0 = %q, want %q", got, want)
	}

	gotSuffixed := NotePath(root, project, date, when, 3)
	wantSuffixed := filepath.Join(root, project, "2026-05-03-143025123-3.md")
	if gotSuffixed != wantSuffixed {
		t.Errorf("NotePath suffix=3 = %q, want %q", gotSuffixed, wantSuffixed)
	}
}

// TestNotePath_NoHostSegment encodes the explicit Phase 2 contract:
// staging is per-host (one host's filesystem only ever sees its own
// staging dir), so the path has NO host segment. The host segment is
// added by Phase 3's wrap-time mirror when projecting staging into
// the shared vault, not here.
func TestNotePath_NoHostSegment(t *testing.T) {
	got := NotePath("/r", "demo", "2026-05-03", time.Now(), 0)
	if strings.Contains(got, "/host") || strings.Contains(got, "_unknown") {
		t.Errorf("NotePath should not include any host segment, got %q", got)
	}
}

// TestNotePath_PureFunction locks NotePath as I/O free: it must not
// stat or create directories. The collision-retry loop in
// session.CaptureFromParsed depends on cheap repeated calls.
func TestNotePath_PureFunction(t *testing.T) {
	root := filepath.Join(t.TempDir(), "never-created")
	when := time.Now()
	got := NotePath(root, "demo", "2026-05-03", when, 0)
	// The function returns a path under root/demo. NotePath itself
	// must not have created either directory; assert the parent
	// chain is still absent on disk.
	if _, err := osStat(filepath.Dir(got)); err == nil {
		t.Errorf("NotePath created parent dir %q (must be I/O free)", filepath.Dir(got))
	}
}
