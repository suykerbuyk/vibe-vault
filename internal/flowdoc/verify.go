// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package flowdoc

import (
	"bufio"
	"fmt"
	"os"
	"path"
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

// lastSingleColonIndex returns the index of the last ':' in s that is
// neither preceded nor followed by another ':'. Returns -1 when no such
// position exists. Used so refs like `dir:Type::method` split between
// `dir` and `Type::method` rather than inside the scope qualifier.
func lastSingleColonIndex(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] != ':' {
			continue
		}
		if i > 0 && s[i-1] == ':' {
			continue
		}
		if i+1 < len(s) && s[i+1] == ':' {
			continue
		}
		return i
	}
	return -1
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

// parseRef splits a step.ref on its LAST single colon and classifies it:
//   - "path"          → bare path
//   - "path:123"      → path:line  (suffix is all digits)
//   - "path:Symbol"   → path:Symbol (suffix is a non-empty non-numeric token)
//
// Colons that are part of a `::` (C++/Rust scope) sequence are NOT
// candidate split points, so refs like `dir:Type::method` parse as
// path=`dir`, symbol=`Type::method` rather than splitting inside the
// scope qualifier.
//
// A trailing-empty suffix ("path:") or a ref with no colon both fall back
// to the bare-path form.
func parseRef(ref string) parsedRef {
	idx := lastSingleColonIndex(ref)
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

// detectLangByPath returns a canonical language tag for the given
// repo-relative path, or "" for an extension this verifier has no
// declaration grammar for. The tag drives declPatterns dispatch.
//
// Recognized: go, c-family (C/C++/headers), cmake (CMakeLists.txt /
// *.cmake), make (Makefile / GNUmakefile / *.mk), rust, python, node
// (JavaScript / TypeScript). Anything else returns "".
func detectLangByPath(rel string) string {
	switch path.Base(rel) {
	case "Makefile", "GNUmakefile":
		return "make"
	case "CMakeLists.txt":
		return "cmake"
	}
	switch strings.ToLower(path.Ext(rel)) {
	case ".go":
		return "go"
	case ".c", ".h", ".cc", ".cpp", ".cxx", ".c++", ".hh", ".hpp", ".hxx", ".h++":
		return "c-family"
	case ".cmake":
		return "cmake"
	case ".mk":
		return "make"
	case ".rs":
		return "rust"
	case ".py":
		return "python"
	case ".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx":
		return "node"
	}
	return ""
}

// declPatterns returns the regex patterns (multi-line mode, with
// regexp-quoted symbol q already substituted) that recognize a
// declaration of symbol q in the given language. Returns nil for an
// unknown language — symbolDeclared treats nil as "do not false-
// positive" (see its doc).
func declPatterns(lang, q string) []string {
	switch lang {
	case "go":
		return []string{
			`(?m)^\s*func\s+(\([^)]*\)\s+)?` + q + `\b`, // func Sym( / func (r T) Sym(
			`(?m)^\s*type\s+` + q + `\b`,                // type Sym ... / type Sym=
			`(?m)^\s*(const\s+|var\s+)?` + q + `\s*=`,   // [const|var] Sym = ... (single)
			`(?m)^\s*(const\s+|var\s+)?` + q + `\s+\S`,  // [const|var] Sym Type ... (typed)
		}
	case "c-family":
		return []string{
			// function decl/def: line begins with type-prefix tokens (return
			// type, qualifiers, namespace) ending in the symbol followed by
			// `(`. RE2 has no lookahead, so a call site like `return
			// foo();` also matches; that is accepted false-positive over
			// false-negative — the verifier wants to confirm presence, not
			// rule out call-site noise.
			`(?m)^\s*[A-Za-z_][\w\s\*\&:<>,]*\b` + q + `\s*\(`,
			`(?m)^\s*(class|struct|union|enum(\s+class)?)\s+` + q + `\b`,
			`(?m)^\s*typedef\b[^;]*\b` + q + `\s*[;\[]`,
			`(?m)^\s*#\s*define\s+` + q + `\b`,
		}
	case "cmake":
		return []string{
			`(?im)^\s*add_(executable|library|custom_target|subdirectory|test|dependencies)\s*\(\s*` + q + `\b`,
			`(?im)^\s*(set|option|function|macro|project|find_package)\s*\(\s*` + q + `\b`,
		}
	case "make":
		// Target lines: "<sym>: deps". Suppresses lines that are clearly
		// variable assignments ("FOO := bar") by requiring the colon to be
		// followed by something other than `=`.
		return []string{
			`(?m)^` + q + `\s*:(?:[^=]|$)`,
		}
	case "rust":
		return []string{
			`(?m)^\s*(pub(\s*\([^)]*\))?\s+)?(async\s+|const\s+|unsafe\s+|extern(\s+"[^"]*")?\s+)*fn\s+` + q + `[\s<\(]`,
			`(?m)^\s*(pub(\s*\([^)]*\))?\s+)?(struct|enum|trait|type|mod|union)\s+` + q + `\b`,
			`(?m)^\s*(pub(\s*\([^)]*\))?\s+)?(const|static)\s+` + q + `\s*[:=]`,
			`(?m)^\s*impl\b[^{]*\b` + q + `\b`,
			`(?m)^\s*macro_rules!\s+` + q + `\b`,
		}
	case "python":
		return []string{
			`(?m)^\s*(async\s+)?def\s+` + q + `\s*\(`,
			`(?m)^\s*class\s+` + q + `[\s\(:]`,
			`(?m)^` + q + `\s*[:=]`,
		}
	case "node":
		return []string{
			`(?m)^\s*(export\s+(default\s+)?)?(async\s+)?function\s*\*?\s+` + q + `\s*\(`,
			`(?m)^\s*(export\s+(default\s+)?)?(abstract\s+)?class\s+` + q + `\b`,
			`(?m)^\s*(export\s+)?(const|let|var)\s+` + q + `\b`,
			`(?m)^\s*(export\s+(default\s+)?)?(type|interface|enum)\s+` + q + `\b`,
		}
	}
	return nil
}

// dirSymbolFileSizeCap bounds how large a single file the directory
// grep will load. Beyond this size the file is skipped — package-level
// symbol resolution should not be at the mercy of a single oversized
// generated file in the directory.
const dirSymbolFileSizeCap = 1 << 20 // 1 MiB

// dirSymbolMaxDepth bounds how deep into dir the recursive grep walks
// for a package-style ref. Rust crates put code in `<crate>/src/...`
// (depth 2+) and C/C++ projects in `<proj>/src/<sub>/...` (depth 3),
// so a small depth cap captures the common idioms without scanning
// unbounded subtrees. Common noise dirs (`vendor`, `target`, `dist`,
// etc.) are pruned regardless of depth.
const dirSymbolMaxDepth = 4

// symbolFoundInDir reports whether any language-recognized file under
// dir (within dirSymbolMaxDepth) declares symbol. Matches package-
// style refs where the model names a crate / package directory and
// the declaration lives one or two levels deeper in `src/` or a
// nested submodule. Common build-output / dependency directories are
// pruned. Files past dirSymbolFileSizeCap or with unknown extensions
// are skipped.
func symbolFoundInDir(dir, symbol string) bool {
	return symbolFoundInDirAt(dir, symbol, 0)
}

func symbolFoundInDirAt(dir, symbol string, depth int) bool {
	if depth > dirSymbolMaxDepth {
		return false
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			if isNoisyDirName(name) {
				continue
			}
			if symbolFoundInDirAt(filepath.Join(dir, name), symbol, depth+1) {
				return true
			}
			continue
		}
		if detectLangByPath(name) == "" {
			continue
		}
		info, err := e.Info()
		if err != nil || info.Size() > dirSymbolFileSizeCap {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		if symbolDeclared(name, string(data), symbol) {
			return true
		}
	}
	return false
}

// isNoisyDirName reports whether name is a build-output / dependency /
// hidden directory that should be skipped during recursive symbol
// grep. The set mirrors repo.go's isNoisePath but operates on the
// basename only since symbolFoundInDir walks one level at a time.
func isNoisyDirName(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	switch name {
	case "vendor", "node_modules", "target", "build", "dist", "out",
		"third_party", "assets", "share", "testdata":
		return true
	}
	return false
}

// symbolDeclared reports whether content declares the named symbol
// according to the per-language grammar selected by detectLangByPath
// against rel. Unknown languages (no grammar in this set) return true
// so the verifier does not produce false-positive missing-symbol
// errors on file types it cannot read — extend declPatterns to add
// strict checking for a new language.
//
// The grammars are deliberately regex-only (no real parser dep) — they
// cover the overwhelming majority of real declarations and intentionally
// prefer over-match (false pass) to false fail.
func symbolDeclared(rel, content, symbol string) bool {
	lang := detectLangByPath(rel)
	for _, candidate := range symbolCandidates(symbol) {
		patterns := declPatterns(lang, regexp.QuoteMeta(candidate))
		if patterns == nil {
			return true
		}
		for _, p := range patterns {
			if regexp.MustCompile(p).MatchString(content) {
				return true
			}
		}
	}
	return false
}

// symbolCandidates expands a possibly-qualified symbol into the bare
// identifiers a per-language grammar can match. Inputs like
// `Type::method` (C++ / Rust) or `Module.Sub.func` (TS / Python) yield
// the rightmost identifier in addition to the original, so the grammar
// matches either the full qualifier or just the method/function name.
// Returns the input itself if it has no qualifier separators.
func symbolCandidates(symbol string) []string {
	out := []string{symbol}
	if i := strings.LastIndex(symbol, "::"); i >= 0 && i+2 < len(symbol) {
		out = append(out, symbol[i+2:])
	}
	if i := strings.LastIndex(symbol, "."); i >= 0 && i+1 < len(symbol) {
		out = append(out, symbol[i+1:])
	}
	return out
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
		if pathExists(abs) {
			return RefIssue{}, false
		}
		// File-missing fallback: the model often picks a plausible-
		// but-wrong filename inside a real package directory. If the
		// parent directory exists, the package name is at least real
		// — downgrade to a weak-match warning rather than a hard
		// missing-file error.
		if parent := filepath.Dir(abs); parent != abs {
			if pi, perr := os.Stat(parent); perr == nil && pi.IsDir() {
				return RefIssue{
					Kind:     IssueWeakMatch,
					Location: loc,
					Ref:      ref,
					Detail:   fmt.Sprintf("file does not exist, but parent directory %s/ exists", filepath.Dir(pr.path)),
				}, true
			}
		}
		return RefIssue{Kind: IssueMissingFile, Location: loc, Ref: ref, Detail: "file does not exist"}, true

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
			// File-missing fallback: a common generator pattern is to
			// emit `dir/file.go:Sym` where `file.go` is wrong but the
			// symbol lives in a sibling file in the same package. If
			// the parent directory exists and any sibling declares the
			// symbol, downgrade to a weak-match warning — the doc is
			// drifted (wrong file name) but its intent resolves.
			if parent := filepath.Dir(abs); parent != abs {
				if pi, perr := os.Stat(parent); perr == nil && pi.IsDir() &&
					symbolFoundInDir(parent, pr.symbol) {
					return RefIssue{
						Kind:     IssueWeakMatch,
						Location: loc,
						Ref:      ref,
						Detail:   fmt.Sprintf("file does not exist, but symbol %q found elsewhere in %s/", pr.symbol, filepath.Dir(pr.path)),
					}, true
				}
			}
			return RefIssue{Kind: IssueMissingFile, Location: loc, Ref: ref, Detail: "file does not exist"}, true
		}
		if info.IsDir() {
			// Package-style ref: `internal/foo:Symbol` — grep every
			// language-recognized file directly under the directory for
			// the symbol. This matches how the flowdoc generator
			// canonically refers to package-scoped declarations whose
			// specific file is incidental.
			if symbolFoundInDir(abs, pr.symbol) {
				return RefIssue{}, false
			}
			return RefIssue{
				Kind:     IssueMissingSymbol,
				Location: loc,
				Ref:      ref,
				Detail:   fmt.Sprintf("symbol %q not declared in any file under directory", pr.symbol),
			}, true
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			return RefIssue{Kind: IssueMissingFile, Location: loc, Ref: ref, Detail: "file could not be read"}, true
		}
		if symbolDeclared(pr.path, string(data), pr.symbol) {
			return RefIssue{}, false
		}
		// Sibling-fallback: file exists and is correct, but the symbol
		// lives in a sibling file in the same package. Common when the
		// model picks an entry-point file but the helper is split out.
		// Downgrade to weak-match — the package name is right, only
		// the precise file is off.
		if parent := filepath.Dir(abs); parent != abs {
			if pi, perr := os.Stat(parent); perr == nil && pi.IsDir() &&
				symbolFoundInDir(parent, pr.symbol) {
				return RefIssue{
					Kind:     IssueWeakMatch,
					Location: loc,
					Ref:      ref,
					Detail:   fmt.Sprintf("symbol %q not in this file but found elsewhere in %s/", pr.symbol, filepath.Dir(pr.path)),
				}, true
			}
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
