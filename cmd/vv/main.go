package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/johns/vibe-vault/internal/config"
	"github.com/johns/vibe-vault/internal/hook"
	"github.com/johns/vibe-vault/internal/scaffold"
	"github.com/johns/vibe-vault/internal/session"
)

const version = "0.2.0"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "init":
		runInit()

	case "hook":
		cfg := mustLoadConfig()
		event := flagValue(os.Args[2:], "--event")
		if err := hook.Handle(cfg, event); err != nil {
			fatal("%v", err)
		}

	case "process":
		cfg := mustLoadConfig()
		if len(os.Args) < 3 {
			fatal("usage: vv process <transcript.jsonl>")
		}
		path := os.Args[2]
		result, err := session.Capture(path, "", "", cfg)
		if err != nil {
			fatal("process: %v", err)
		}
		if result.Skipped {
			fmt.Printf("skipped: %s\n", result.Reason)
		} else {
			fmt.Printf("created: %s (%s)\n", result.NotePath, result.Title)
		}

	case "version":
		fmt.Printf("vv v%s (vibe-vault)\n", version)

	case "help", "--help", "-h":
		usage()

	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		usage()
		os.Exit(1)
	}
}

func runInit() {
	args := os.Args[2:]
	gitInit := hasFlag(args, "--git")
	args = removeFlag(args, "--git")

	target := "./vibe-vault"
	if len(args) > 0 {
		target = args[0]
	}

	absTarget, err := filepath.Abs(target)
	if err != nil {
		fatal("resolve path: %v", err)
	}

	fmt.Printf("Creating vault at %s\n", absTarget)

	if err := scaffold.Init(absTarget, scaffold.Options{GitInit: gitInit}); err != nil {
		fatal("init: %v", err)
	}

	cfgPath, err := config.WriteDefault(absTarget)
	if err != nil {
		fatal("write config: %v", err)
	}

	fmt.Println("\nDone! Next steps:")
	fmt.Printf("  1. Open %s in Obsidian\n", absTarget)
	fmt.Println("  2. Install community plugins: Dataview, Templater")
	fmt.Println("  3. Add hook to ~/.claude/settings.json:")
	fmt.Println(`     {"hooks": {"SessionEnd": [{"matcher": "", "hooks": [{"type": "command", "command": "vv hook"}]}]}}`)
	fmt.Printf("\nConfig written to %s\n", cfgPath)
}

func usage() {
	fmt.Fprintf(os.Stderr, `vv v%s — vibe-vault session capture

Usage:
  vv init [path] [--git]     Create a new vault (default: ./vibe-vault)
  vv hook [--event <name>]   Hook mode (reads stdin from Claude Code)
  vv process <file.jsonl>    Process a single transcript file
  vv version                 Print version
  vv help                    Show this help

Hook integration (settings.json):
  {"type": "command", "command": "vv hook"}

Configuration: ~/.config/vibe-vault/config.toml
`, version)
}

func mustLoadConfig() config.Config {
	cfg, err := config.Load()
	if err != nil {
		fatal("load config: %v", err)
	}
	return cfg
}

func flagValue(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

func removeFlag(args []string, flag string) []string {
	var out []string
	for _, a := range args {
		if a != flag {
			out = append(out, a)
		}
	}
	return out
}

func fatal(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "vv: "+format+"\n", args...)
	os.Exit(1)
}
