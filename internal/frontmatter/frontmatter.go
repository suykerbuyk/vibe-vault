// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

// Package frontmatter parses YAML-style "---"-delimited frontmatter blocks
// at the start of markdown files.
//
// It is a minimal state machine: values are returned as raw strings with
// matching edge quotes stripped, no type coercion is performed, and the
// body is returned as the lines following the closing delimiter. Callers
// layer their own schema validation, section extraction, and typed mapping
// on top of Result.Fields / Result.Body.
//
// Options selects between two modes:
//
//   - permissive (zero value): a missing opening "---" makes the entire
//     input body, with empty Fields; an unterminated block is silently
//     accepted with whatever Fields were scanned. Matches the historical
//     noteparse behavior.
//
//   - strict (RequireDelimiter + RequireClose): missing or unclosed
//     delimiters return ErrMissingOpener / ErrUnterminated. Matches the
//     historical knowledge/learnings parseLearning behavior.
package frontmatter

import (
	"bufio"
	"errors"
	"io"
	"strings"
)

// Result is the parsed frontmatter output.
type Result struct {
	// Fields holds the raw key/value pairs from the frontmatter block.
	// Always non-nil. Empty when no frontmatter is present (permissive
	// mode) or the block is empty. Values have surrounding matching
	// single or double quotes removed via StripQuotes.
	Fields map[string]string

	// Body holds the lines that appear after the closing delimiter, or
	// all lines when permissive mode saw no opener. Nil when the input
	// is empty or fully consumed by the frontmatter block.
	Body []string
}

// Options configures Parse. Zero value = permissive (noteparse-style)
// parsing with bufio's default line-buffer limit.
type Options struct {
	// RequireDelimiter causes Parse to return ErrMissingOpener if the
	// first line is not "---" (after TrimSpace). When false, inputs
	// without an opening delimiter are treated as body-only.
	RequireDelimiter bool

	// RequireClose causes Parse to return ErrUnterminated if EOF is
	// reached while still scanning inside the frontmatter block. When
	// false, any fields scanned before EOF are returned with no error.
	RequireClose bool

	// MaxLineBytes sets the scanner's per-line buffer ceiling. Zero
	// leaves bufio's default (~64KB) in place.
	MaxLineBytes int
}

// ErrMissingOpener is returned (strict mode only) when no opening "---"
// delimiter was found.
var ErrMissingOpener = errors.New("frontmatter: missing opening delimiter")

// ErrUnterminated is returned (strict mode only) when the input ends
// while Parse is still inside the frontmatter block.
var ErrUnterminated = errors.New("frontmatter: unterminated block")

// Parse consumes r and returns the parsed result. See Options for mode
// selection.
func Parse(r io.Reader, opts Options) (*Result, error) {
	scanner := bufio.NewScanner(r)
	if opts.MaxLineBytes > 0 {
		scanner.Buffer(make([]byte, 0, 64*1024), opts.MaxLineBytes)
	}

	res := &Result{Fields: make(map[string]string)}

	const (
		beforeOpener = 0
		insideBlock  = 1
		afterCloser  = 2
	)
	state := beforeOpener

	for scanner.Scan() {
		line := scanner.Text()
		switch state {
		case beforeOpener:
			if strings.TrimSpace(line) == "---" {
				state = insideBlock
				continue
			}
			if opts.RequireDelimiter {
				return nil, ErrMissingOpener
			}
			res.Body = append(res.Body, line)
			state = afterCloser
		case insideBlock:
			if strings.TrimSpace(line) == "---" {
				state = afterCloser
				continue
			}
			if idx := strings.IndexByte(line, ':'); idx > 0 {
				key := strings.TrimSpace(line[:idx])
				val := StripQuotes(strings.TrimSpace(line[idx+1:]))
				res.Fields[key] = val
			}
		case afterCloser:
			res.Body = append(res.Body, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	switch {
	case state == beforeOpener && opts.RequireDelimiter:
		return nil, ErrMissingOpener
	case state == insideBlock && opts.RequireClose:
		return nil, ErrUnterminated
	}
	return res, nil
}

// StripQuotes removes a single pair of matching ASCII quotes (' or ")
// from the edges of s. Inputs without matching edge quotes are returned
// unchanged.
func StripQuotes(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
