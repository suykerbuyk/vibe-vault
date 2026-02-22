package main

import (
	"fmt"
	"os"

	"github.com/johns/sesscap/internal/config"
	"github.com/johns/sesscap/internal/hook"
	"github.com/johns/sesscap/internal/session"
)

const version = "0.1.0"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	cfg, err := config.Load()
	if err != nil {
		fatal("load config: %v", err)
	}

	switch os.Args[1] {
	case "hook":
		event := flagValue(os.Args[2:], "--event")
		if err := hook.Handle(cfg, event); err != nil {
			fatal("%v", err)
		}

	case "process":
		if len(os.Args) < 3 {
			fatal("usage: sesscap process <transcript.jsonl>")
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
		fmt.Printf("sesscap v%s\n", version)

	case "help", "--help", "-h":
		usage()

	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `sesscap v%s — Claude Code session capture

Usage:
  sesscap hook [--event <name>]    Hook mode (reads stdin from Claude Code)
  sesscap process <file.jsonl>     Process a single transcript file
  sesscap version                  Print version
  sesscap help                     Show this help

Hook integration (settings.json):
  {"type": "command", "command": "sesscap hook"}

Configuration: ~/.config/sesscap/config.toml
`, version)
}

func flagValue(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func fatal(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "sesscap: "+format+"\n", args...)
	os.Exit(1)
}
