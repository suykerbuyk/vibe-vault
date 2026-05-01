// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/suykerbuyk/vibe-vault/internal/config"
	"github.com/suykerbuyk/vibe-vault/internal/help"
	"github.com/suykerbuyk/vibe-vault/internal/mcp"
	"github.com/suykerbuyk/vibe-vault/internal/surface"
)

// goldenPath is the repo-relative location of the golden tool-surface manifest.
const goldenPath = "internal/mcp/tool_surface.golden.json"

// runInternalVerifyToolSurface implements `vv internal verify-tool-surface
// [--update-golden]`.
//
// Default (strict) mode:
//   - exit 0 if live tool inventory matches the golden manifest
//   - exit 1 with a human-readable diff if drift is detected
//   - exit 1 with an actionable hint if MCPSurfaceVersion was bumped but
//     golden has not been refreshed
//
// --update-golden mode:
//   - refresh the manifest only when MCPSurfaceVersion has been bumped past
//     the recorded golden surface (or when the manifest doesn't exist yet)
//   - exit 1 with the same drift message if drift is present without a bump
func runInternalVerifyToolSurface(args []string) {
	updateGolden := false
	for _, a := range args {
		if a == "--update-golden" {
			updateGolden = true
		}
	}

	cfg, _ := config.Load()
	logger := log.New(io.Discard, "", 0)
	srv := mcp.NewServer(mcp.ServerInfo{Name: "vibe-vault", Version: help.Version}, logger)
	mcp.RegisterAllTools(srv, cfg)

	live := mcp.LiveManifest(srv, surface.MCPSurfaceVersion)

	abspath, err := filepath.Abs(goldenPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "verify-tool-surface: %v\n", err)
		os.Exit(2)
	}

	golden, err := mcp.LoadGolden(abspath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "verify-tool-surface: %v\n", err)
		os.Exit(2)
	}

	diff := mcp.Diff(live, golden)

	if updateGolden {
		// Strict sequence: load OLD golden, classify diff, require version bump,
		// only-then write.
		if !diff.Empty() && surface.MCPSurfaceVersion <= golden.SurfaceVersion {
			fmt.Fprint(os.Stderr, mcp.FormatDriftError(diff, surface.MCPSurfaceVersion, golden.SurfaceVersion))
			os.Exit(1)
		}
		if diff.Empty() {
			fmt.Println("verify-tool-surface: no diff vs golden; nothing to update")
			return
		}
		if err := mcp.WriteGolden(abspath, live); err != nil {
			fmt.Fprintf(os.Stderr, "verify-tool-surface: %v\n", err)
			os.Exit(2)
		}
		fmt.Printf("verify-tool-surface: golden updated to surface=%d (%d tools)\n",
			live.SurfaceVersion, len(live.Tools))
		return
	}

	// Strict mode (default).
	if diff.Empty() {
		fmt.Printf("verify-tool-surface: live matches golden (surface=%d, %d tools)\n",
			live.SurfaceVersion, len(live.Tools))
		return
	}

	if surface.MCPSurfaceVersion > golden.SurfaceVersion {
		// Bump is in place but golden hasn't been refreshed yet — actionable hint.
		fmt.Fprintf(os.Stderr,
			"verify-tool-surface: surface bumped (binary=%d > golden=%d) but golden not refreshed.\n"+
				"    action: run 'vv internal verify-tool-surface --update-golden' to refresh.\n",
			surface.MCPSurfaceVersion, golden.SurfaceVersion)
		os.Exit(1)
	}

	fmt.Fprint(os.Stderr, mcp.FormatDriftError(diff, surface.MCPSurfaceVersion, golden.SurfaceVersion))
	os.Exit(1)
}
