// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package frontmatter

import (
	"errors"
	"strings"
	"testing"
)

func TestParseBasicFrontmatter(t *testing.T) {
	input := "---\nname: Alpha\ntype: user\n---\nbody line one\nbody line two\n"
	res, err := Parse(strings.NewReader(input), Options{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := res.Fields["name"]; got != "Alpha" {
		t.Errorf("Fields[name] = %q, want Alpha", got)
	}
	if got := res.Fields["type"]; got != "user" {
		t.Errorf("Fields[type] = %q, want user", got)
	}
	if len(res.Body) != 2 {
		t.Fatalf("Body len = %d, want 2", len(res.Body))
	}
	if res.Body[0] != "body line one" || res.Body[1] != "body line two" {
		t.Errorf("Body = %v", res.Body)
	}
}

func TestParseFieldsAlwaysNonNil(t *testing.T) {
	res, err := Parse(strings.NewReader(""), Options{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if res.Fields == nil {
		t.Error("Fields must be non-nil even for empty input")
	}
	if len(res.Fields) != 0 {
		t.Errorf("Fields len = %d, want 0", len(res.Fields))
	}
}

func TestPermissiveNoOpenerTreatedAsBody(t *testing.T) {
	input := "# Just a heading\n\nSome body text.\n"
	res, err := Parse(strings.NewReader(input), Options{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(res.Fields) != 0 {
		t.Errorf("Fields len = %d, want 0", len(res.Fields))
	}
	want := []string{"# Just a heading", "", "Some body text."}
	if len(res.Body) != len(want) {
		t.Fatalf("Body len = %d, want %d", len(res.Body), len(want))
	}
	for i, w := range want {
		if res.Body[i] != w {
			t.Errorf("Body[%d] = %q, want %q", i, res.Body[i], w)
		}
	}
}

func TestPermissiveEmptyInput(t *testing.T) {
	res, err := Parse(strings.NewReader(""), Options{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(res.Body) != 0 {
		t.Errorf("Body len = %d, want 0", len(res.Body))
	}
}

func TestPermissiveUnterminatedSilentlyAccepted(t *testing.T) {
	// Historical noteparse behavior: no closer means EOF inside the
	// block, fields parsed so far survive, no error.
	input := "---\nname: X\ntype: user\n"
	res, err := Parse(strings.NewReader(input), Options{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if res.Fields["name"] != "X" {
		t.Errorf("Fields[name] = %q, want X", res.Fields["name"])
	}
	if res.Fields["type"] != "user" {
		t.Errorf("Fields[type] = %q, want user", res.Fields["type"])
	}
	if len(res.Body) != 0 {
		t.Errorf("Body len = %d, want 0 for unterminated", len(res.Body))
	}
}

func TestStrictMissingOpenerReturnsError(t *testing.T) {
	input := "not a delimiter\nsecond line\n"
	_, err := Parse(strings.NewReader(input), Options{RequireDelimiter: true})
	if !errors.Is(err, ErrMissingOpener) {
		t.Errorf("err = %v, want ErrMissingOpener", err)
	}
}

func TestStrictEmptyInputReturnsError(t *testing.T) {
	_, err := Parse(strings.NewReader(""), Options{RequireDelimiter: true})
	if !errors.Is(err, ErrMissingOpener) {
		t.Errorf("err = %v, want ErrMissingOpener", err)
	}
}

func TestStrictUnterminatedReturnsError(t *testing.T) {
	input := "---\nname: X\n"
	_, err := Parse(strings.NewReader(input), Options{RequireDelimiter: true, RequireClose: true})
	if !errors.Is(err, ErrUnterminated) {
		t.Errorf("err = %v, want ErrUnterminated", err)
	}
}

func TestStrictWellFormedSucceeds(t *testing.T) {
	input := "---\nname: X\ntype: user\n---\nbody\n"
	res, err := Parse(strings.NewReader(input), Options{RequireDelimiter: true, RequireClose: true})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if res.Fields["name"] != "X" {
		t.Errorf("Fields[name] = %q", res.Fields["name"])
	}
}

func TestLeadingBlankLineBeforeOpenerIsBodyInPermissive(t *testing.T) {
	// A blank line before "---" prevents opener detection in both
	// historical parsers; that preserved behavior lives on.
	input := "\n---\nname: X\n---\n"
	res, err := Parse(strings.NewReader(input), Options{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(res.Fields) != 0 {
		t.Errorf("Fields should be empty since opener not matched, got %v", res.Fields)
	}
}

func TestLeadingBlankLineBeforeOpenerIsErrorInStrict(t *testing.T) {
	input := "\n---\nname: X\n---\n"
	_, err := Parse(strings.NewReader(input), Options{RequireDelimiter: true})
	if !errors.Is(err, ErrMissingOpener) {
		t.Errorf("err = %v, want ErrMissingOpener", err)
	}
}

func TestColonInValueFirstColonWins(t *testing.T) {
	input := "---\nurl: http://example.com:80/path\n---\n"
	res, err := Parse(strings.NewReader(input), Options{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := res.Fields["url"]; got != "http://example.com:80/path" {
		t.Errorf("Fields[url] = %q", got)
	}
}

func TestLineWithoutColonSkipped(t *testing.T) {
	input := "---\nname: X\nbare-word-no-colon\ntype: user\n---\n"
	res, err := Parse(strings.NewReader(input), Options{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(res.Fields) != 2 {
		t.Errorf("Fields len = %d, want 2 (bare word ignored)", len(res.Fields))
	}
}

func TestEmptyValueProducesEmptyString(t *testing.T) {
	input := "---\nname:\ntype: user\n---\n"
	res, err := Parse(strings.NewReader(input), Options{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if v, ok := res.Fields["name"]; !ok || v != "" {
		t.Errorf("Fields[name] = %q, ok=%v; want \"\", true", v, ok)
	}
}

func TestKeyWithLeadingWhitespaceTrimmed(t *testing.T) {
	input := "---\n  name: X\n---\n"
	res, err := Parse(strings.NewReader(input), Options{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if res.Fields["name"] != "X" {
		t.Errorf("whitespace-prefixed key not trimmed: %v", res.Fields)
	}
}

func TestBodyPreservesLeadingBlankLine(t *testing.T) {
	// knowledge trims a leading blank body line; noteparse preserves it.
	// The frontmatter package preserves — trimming is caller-side.
	input := "---\nname: X\n---\n\ncontent\n"
	res, err := Parse(strings.NewReader(input), Options{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(res.Body) != 2 {
		t.Fatalf("Body len = %d, want 2 (leading blank preserved)", len(res.Body))
	}
	if res.Body[0] != "" {
		t.Errorf("Body[0] = %q, want empty", res.Body[0])
	}
	if res.Body[1] != "content" {
		t.Errorf("Body[1] = %q", res.Body[1])
	}
}

func TestMaxLineBytesRespected(t *testing.T) {
	// 200KB value, well over bufio's 64KB default.
	longVal := strings.Repeat("x", 200*1024)
	input := "---\nname: " + longVal + "\n---\n"

	// Default buffer should error.
	_, err := Parse(strings.NewReader(input), Options{})
	if err == nil {
		t.Error("expected scanner buffer error with default limit")
	}

	// Generous limit should parse.
	res, err := Parse(strings.NewReader(input), Options{MaxLineBytes: 1 << 20})
	if err != nil {
		t.Fatalf("Parse with large buffer: %v", err)
	}
	if res.Fields["name"] != longVal {
		t.Errorf("long value not preserved")
	}
}

func TestStripQuotesDouble(t *testing.T) {
	if got := StripQuotes(`"hello"`); got != "hello" {
		t.Errorf("StripQuotes(\"hello\") = %q", got)
	}
}

func TestStripQuotesSingle(t *testing.T) {
	if got := StripQuotes(`'hello'`); got != "hello" {
		t.Errorf("StripQuotes('hello') = %q", got)
	}
}

func TestStripQuotesNoQuotes(t *testing.T) {
	if got := StripQuotes(`hello`); got != "hello" {
		t.Errorf("StripQuotes(hello) = %q", got)
	}
}

func TestStripQuotesMismatched(t *testing.T) {
	// Mismatched edges = leave unchanged.
	cases := []string{`"hello'`, `'hello"`, `"hello`, `hello"`}
	for _, in := range cases {
		if got := StripQuotes(in); got != in {
			t.Errorf("StripQuotes(%q) = %q, want unchanged", in, got)
		}
	}
}

func TestStripQuotesEmpty(t *testing.T) {
	if got := StripQuotes(""); got != "" {
		t.Errorf("StripQuotes(\"\") = %q", got)
	}
}

func TestStripQuotesOnlyQuotes(t *testing.T) {
	if got := StripQuotes(`""`); got != "" {
		t.Errorf("StripQuotes(\"\") = %q, want empty", got)
	}
	if got := StripQuotes(`''`); got != "" {
		t.Errorf(`StripQuotes('') = %q, want empty`, got)
	}
}

func TestStripQuotesSingleChar(t *testing.T) {
	// Single-character input can't hold matching edges.
	if got := StripQuotes(`"`); got != `"` {
		t.Errorf(`StripQuotes(") = %q, want unchanged`, got)
	}
}

func TestQuotedValuesInFrontmatter(t *testing.T) {
	input := "---\nname: \"Quoted Name\"\ndesc: 'single quotes'\nbare: nobare\n---\n"
	res, err := Parse(strings.NewReader(input), Options{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if res.Fields["name"] != "Quoted Name" {
		t.Errorf("Fields[name] = %q", res.Fields["name"])
	}
	if res.Fields["desc"] != "single quotes" {
		t.Errorf("Fields[desc] = %q", res.Fields["desc"])
	}
	if res.Fields["bare"] != "nobare" {
		t.Errorf("Fields[bare] = %q", res.Fields["bare"])
	}
}

func TestEmptyFrontmatterBlockReturnsEmptyFields(t *testing.T) {
	input := "---\n---\nbody\n"
	res, err := Parse(strings.NewReader(input), Options{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(res.Fields) != 0 {
		t.Errorf("Fields len = %d, want 0", len(res.Fields))
	}
	if len(res.Body) != 1 || res.Body[0] != "body" {
		t.Errorf("Body = %v", res.Body)
	}
}

func TestNoBodyAfterCloser(t *testing.T) {
	input := "---\nname: X\n---\n"
	res, err := Parse(strings.NewReader(input), Options{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(res.Body) != 0 {
		t.Errorf("Body len = %d, want 0 (nothing after closer)", len(res.Body))
	}
}
