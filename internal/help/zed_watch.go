// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package help

var CmdZedWatch = Command{
	Name:     "zed watch",
	Synopsis: "watch Zed threads database and auto-capture sessions",
	Brief:    "Watch Zed threads DB and auto-capture",
	Usage:    "vv zed watch [--db PATH] [--debounce DURATION] [--project NAME]",
	Flags: []Flag{
		{Name: "--db <path>", Desc: "Path to threads.db (default: ~/.local/share/zed/threads/threads.db)"},
		{Name: "--debounce <duration>", Desc: "Quiet period before capture (default: from config or 5m)"},
		{Name: "--project <name>", Desc: "Only capture threads for this project"},
	},
	Description: `Watches the Zed threads database for changes and automatically captures
new or updated threads after a quiet period. Uses filesystem notifications
on the WAL file to detect writes with minimal overhead.

The capture pipeline is identical to "vv zed backfill" — threads are
detected, converted, dedup-checked against the session index, and
captured as session notes.

Runs until interrupted (Ctrl-C / SIGTERM).`,
	Examples: []string{
		"vv zed watch                           Watch with default settings",
		"vv zed watch --debounce 2m             Capture after 2 minutes of quiet",
		"vv zed watch --project myproj          Only capture threads for myproj",
	},
	SeeAlso: []string{"vv(1)", "vv-zed(1)", "vv-zed-backfill(1)"},
}
