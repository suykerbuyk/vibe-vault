// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

// Package templates provides the embedded agentctx template files.
// These are the canonical defaults used by `vv context init` and the
// template registry. Edit the .md files directly to change defaults.
package templates

import "embed"

//go:embed all:agentctx
var agentctx embed.FS

// AgentctxFS returns the embedded agentctx template filesystem.
// Files are rooted under "agentctx/", e.g. "agentctx/workflow.md".
func AgentctxFS() embed.FS { return agentctx }
