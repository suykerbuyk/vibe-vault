// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package main

import (
	"fmt"
	"io"

	"github.com/suykerbuyk/vibe-vault/internal/templates"
)

// runCommand handles `vv command <subcommand> [args]`.
//
// Currently supports:
//
//	vv command get <name>   print the body of agentctx/commands/<name>.md
//
// The body comes from vv's embedded templates, so the output is independent
// of cwd, vault state, or per-project sync. Designed for shellout consumers
// like the Vibe Vault Zed extension, whose WASM sandbox cannot read project
// files arbitrarily and proxies slash commands through this subcommand.
func runCommand(args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: vv command get <name>")
	}
	switch args[0] {
	case "get":
		if len(args) < 2 {
			return fmt.Errorf("usage: vv command get <name>")
		}
		return runCommandGet(args[1], out)
	default:
		return fmt.Errorf("unknown subcommand: %q (expected: get)", args[0])
	}
}

// runCommandGet prints the canonical body of agentctx/commands/<name>.md to out.
func runCommandGet(name string, out io.Writer) error {
	relPath := "agentctx/commands/" + name + ".md"
	reg := templates.New()
	body, ok := reg.DefaultContent(relPath)
	if !ok {
		return fmt.Errorf("unknown command: %q", name)
	}
	if _, err := out.Write(body); err != nil {
		return err
	}
	return nil
}
