// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package flowdoc

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// RefIssueKind classifies a single drift finding from VerifyRefs. Every
// kind except IssueWeakMatch is a hard error (see RefIssue.IsError).
type RefIssueKind string

const (
	// IssueMissingFile — a step ref path or node path doesn't exist on disk.
	IssueMissingFile RefIssueKind = "missing-file"
	// IssueMissingSymbol — a path:Symbol ref whose symbol isn't declared in the file.
	IssueMissingSymbol RefIssueKind = "missing-symbol"
	// IssueOutOfRangeLine — a path:line ref whose line number is past EOF.
	IssueOutOfRangeLine RefIssueKind = "out-of-range-line"
	// IssueDanglingNodeRef — a flow.nodes[] / step.from / step.to id not in nodes[].
	IssueDanglingNodeRef RefIssueKind = "dangling-node-ref"
	// IssueWeakMatch — a path:line ref whose line exists but isn't a decl (WARNING).
	IssueWeakMatch RefIssueKind = "weak-match"
)

// RefIssue is one finding produced by VerifyRefs.
type RefIssue struct {
	Kind     RefIssueKind
	Location string // human-facing: e.g. `flow "wrap" step 3` or `node "internal/mcp"`
	Ref      string // the offending ref/path string
	Detail   string // extra context
}

// IsError reports whether the issue is a hard error (blocks). Everything
// except a weak-match warning is an error.
func (i RefIssue) IsError() bool {
	return i.Kind != IssueWeakMatch
}

// declLineRe recognizes a Go declaration line for the path:line weak-match
// heuristic. We accept the trimmed line starting with `func ` or `type `,
// or containing `func (` (a method with a receiver).
var declLineRe = regexp.MustCompile(`func \(`)

// isParenthesizedPath reports whether a node path is a sentinel like
// "(external)" or "(filesystem)" rather than a real on-disk path.
func isParenthesizedPath(p string) bool {
	p = strings.TrimSpace(p)
	return strings.HasPrefix(p, "(") && strings.HasSuffix(p, ")")
}

// pathExists reports whether name exists on disk (file or directory).
func pathExists(name string) bool {
	_, err := os.Stat(name)
	return err == nil
}

// refForm classifies a step ref string.
type refForm int

const (
	refBarePath refForm = iota
	refPathLine
	refPathSymbol
)

// parsedRef is the result of splitting a step.ref on its last colon.
type parsedRef struct {
	form   refForm
	path   string
	line   int    // valid when form == refPathLine
	symbol string // valid when form == refPathSymbol
}

// allDigits reports whether s is non-empty and entirely ASCII digits.
func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// parseRef splits a step.ref on its LAST colon and classifies it:
//   - "path"          → bare path
//   - "path:123"      → path:line  (suffix is all digits)
//   - "path:Symbol"   → path:Symbol (suffix is a non-empty non-numeric token)
//
// A trailing-empty suffix ("path:") or a ref with no colon both fall back
// to the bare-path form.
func parseRef(ref string) parsedRef {
	idx := strings.LastIndex(ref, ":")
	if idx < 0 {
		return parsedRef{form: refBarePath, path: ref}
	}
	path := ref[:idx]
	suffix := ref[idx+1:]
	switch {
	case suffix == "":
		return parsedRef{form: refBarePath, path: ref}
	case allDigits(suffix):
		n := 0
		for _, r := range suffix {
			n = n*10 + int(r-'0')
		}
		return parsedRef{form: refPathLine, path: path, line: n}
	default:
		return parsedRef{form: refPathSymbol, path: path, symbol: suffix}
	}
}

// readLines returns the file's lines (without trailing newlines) and the
// total line count. A trailing newline does not produce a final empty
// line. An unreadable file yields a nil slice and a false ok.
func readLines(name string) (lines []string, ok bool) {
	f, err := os.Open(name)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if sc.Err() != nil {
		return nil, false
	}
	return lines, true
}

// symbolDeclared reports whether content declares the Go symbol via a
// func, method, or type/const/var form. The regex set is pragmatic — it
// covers the overwhelming majority of real declarations.
func symbolDeclared(content, symbol string) bool {
	q := regexp.QuoteMeta(symbol)
	patterns := []string{
		`(?m)^\s*func\s+(\([^)]*\)\s+)?` + q + `\b`, // func Sym( / func (r T) Sym(
		`(?m)^\s*type\s+` + q + `\b`,                // type Sym ... / type Sym=
		`(?m)^\s*(const\s+|var\s+)?` + q + `\s*=`,   // [const|var] Sym = ... (single)
		`(?m)^\s*(const\s+|var\s+)?` + q + `\s+\S`,  // [const|var] Sym Type ... (typed)
	}
	for _, p := range patterns {
		if regexp.MustCompile(p).MatchString(content) {
			return true
		}
	}
	return false
}

// VerifyRefs walks a FlowDoc and reports every ref that has drifted away
// from the source tree rooted at repoRoot. It performs no LLM calls and
// touches only the filesystem — deterministic and zero-cost.
//
// The four hard-error kinds (missing-file, missing-symbol, out-of-range-line,
// dangling-node-ref) indicate the doc no longer matches the code. The lone
// warning kind (weak-match) flags a path:line ref that resolves to a line
// which isn't a declaration — often a deliberate pointer into a function
// body, so it is informational only.
func VerifyRefs(doc *FlowDoc, repoRoot string) []RefIssue {
	var issues []RefIssue
	if doc == nil {
		return issues
	}

	// Build the node id set once for cross-reference checks.
	nodeIDs := make(map[string]struct{}, len(doc.Nodes))
	for _, n := range doc.Nodes {
		nodeIDs[n.ID] = struct{}{}
	}

	// 1. nodes[].path — real paths must exist on disk.
	for _, n := range doc.Nodes {
		if n.Kind == "external" || isParenthesizedPath(n.Path) {
			continue
		}
		if !pathExists(filepath.Join(repoRoot, n.Path)) {
			issues = append(issues, RefIssue{
				Kind:     IssueMissingFile,
				Location: fmt.Sprintf("node %q", n.ID),
				Ref:      n.Path,
				Detail:   "path does not exist",
			})
		}
	}

	// 2 & 3. flows[].nodes[] and flows[].steps[].from/.to — dangling ids.
	// 4. flows[].steps[].ref — file/symbol/line resolution.
	for _, f := range doc.Flows {
		for _, nodeID := range f.Nodes {
			if _, ok := nodeIDs[nodeID]; !ok {
				issues = append(issues, RefIssue{
					Kind:     IssueDanglingNodeRef,
					Location: fmt.Sprintf("flow %q nodes[]", f.Slug),
					Ref:      nodeID,
					Detail:   "node id not in top-level nodes[]",
				})
			}
		}

		for i, s := range f.Steps {
			stepNo := i + 1
			if _, ok := nodeIDs[s.From]; !ok {
				issues = append(issues, RefIssue{
					Kind:     IssueDanglingNodeRef,
					Location: fmt.Sprintf("flow %q step %d from", f.Slug, stepNo),
					Ref:      s.From,
					Detail:   "node id not in top-level nodes[]",
				})
			}
			if _, ok := nodeIDs[s.To]; !ok {
				issues = append(issues, RefIssue{
					Kind:     IssueDanglingNodeRef,
					Location: fmt.Sprintf("flow %q step %d to", f.Slug, stepNo),
					Ref:      s.To,
					Detail:   "node id not in top-level nodes[]",
				})
			}

			if s.Ref == "" {
				continue
			}
			if issue, ok := verifyStepRef(f.Slug, stepNo, s.Ref, repoRoot); ok {
				issues = append(issues, issue)
			}
		}
	}

	return issues
}

// verifyStepRef resolves a single non-empty step.ref against repoRoot,
// returning at most one issue. ok is false when the ref is clean.
func verifyStepRef(slug string, stepNo int, ref, repoRoot string) (RefIssue, bool) {
	loc := fmt.Sprintf("flow %q step %d", slug, stepNo)
	pr := parseRef(ref)
	abs := filepath.Join(repoRoot, pr.path)

	switch pr.form {
	case refBarePath:
		if !pathExists(abs) {
			return RefIssue{Kind: IssueMissingFile, Location: loc, Ref: ref, Detail: "file does not exist"}, true
		}
		return RefIssue{}, false

	case refPathLine:
		info, err := os.Stat(abs)
		if err != nil {
			return RefIssue{Kind: IssueMissingFile, Location: loc, Ref: ref, Detail: "file does not exist"}, true
		}
		if info.IsDir() {
			return RefIssue{Kind: IssueMissingFile, Location: loc, Ref: ref, Detail: "path is a directory, cannot resolve line"}, true
		}
		lines, readOK := readLines(abs)
		if !readOK {
			return RefIssue{Kind: IssueMissingFile, Location: loc, Ref: ref, Detail: "file could not be read"}, true
		}
		if pr.line > len(lines) {
			return RefIssue{
				Kind:     IssueOutOfRangeLine,
				Location: loc,
				Ref:      ref,
				Detail:   fmt.Sprintf("line %d beyond EOF (%d lines)", pr.line, len(lines)),
			}, true
		}
		text := strings.TrimSpace(lines[pr.line-1])
		if strings.HasPrefix(text, "func ") || strings.HasPrefix(text, "type ") || declLineRe.MatchString(text) {
			return RefIssue{}, false
		}
		return RefIssue{
			Kind:     IssueWeakMatch,
			Location: loc,
			Ref:      ref,
			Detail:   fmt.Sprintf("line %d is not a declaration", pr.line),
		}, true

	case refPathSymbol:
		info, err := os.Stat(abs)
		if err != nil {
			return RefIssue{Kind: IssueMissingFile, Location: loc, Ref: ref, Detail: "file does not exist"}, true
		}
		if info.IsDir() {
			return RefIssue{
				Kind:     IssueMissingSymbol,
				Location: loc,
				Ref:      ref,
				Detail:   "path is a directory, cannot resolve symbol",
			}, true
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			return RefIssue{Kind: IssueMissingFile, Location: loc, Ref: ref, Detail: "file could not be read"}, true
		}
		if symbolDeclared(string(data), pr.symbol) {
			return RefIssue{}, false
		}
		return RefIssue{
			Kind:     IssueMissingSymbol,
			Location: loc,
			Ref:      ref,
			Detail:   fmt.Sprintf("symbol %q not declared in file", pr.symbol),
		}, true
	}

	return RefIssue{}, false
}

// FormatRefIssues renders issues as a human-readable multi-line report,
// errors first then warnings. Returns the empty string when issues is
// empty. Each line has the form:
//
//	[missing-file] flow "wrap" step 3: internal/gone/x.go — file does not exist
func FormatRefIssues(issues []RefIssue) string {
	if len(issues) == 0 {
		return ""
	}

	var errs, warns []RefIssue
	for _, i := range issues {
		if i.IsError() {
			errs = append(errs, i)
		} else {
			warns = append(warns, i)
		}
	}

	line := func(i RefIssue) string {
		return fmt.Sprintf("  [%s] %s: %s — %s", i.Kind, i.Location, i.Ref, i.Detail)
	}

	var b strings.Builder
	if len(errs) > 0 {
		fmt.Fprintf(&b, "errors (%d):\n", len(errs))
		sort.SliceStable(errs, func(a, c int) bool { return errs[a].Location < errs[c].Location })
		for _, i := range errs {
			b.WriteString(line(i))
			b.WriteByte('\n')
		}
	}
	if len(warns) > 0 {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "warnings (%d):\n", len(warns))
		sort.SliceStable(warns, func(a, c int) bool { return warns[a].Location < warns[c].Location })
		for _, i := range warns {
			b.WriteString(line(i))
			b.WriteByte('\n')
		}
	}
	return b.String()
}
