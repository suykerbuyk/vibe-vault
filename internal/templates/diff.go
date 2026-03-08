// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package templates

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Diff returns a unified diff between the built-in default and the vault copy
// for a single template. Returns empty string if files are identical.
// Returns an error if the template name is unknown or the vault file is missing.
func (r *Registry) Diff(vaultTemplatesDir, relPath string) (string, error) {
	defaultContent, ok := r.DefaultContent(relPath)
	if !ok {
		return "", fmt.Errorf("unknown template: %s", relPath)
	}

	vaultPath := filepath.Join(vaultTemplatesDir, relPath)
	vaultContent, err := os.ReadFile(vaultPath)
	if err != nil {
		return "", fmt.Errorf("read vault template: %w", err)
	}

	return unifiedDiff(
		fmt.Sprintf("a/%s (default)", relPath),
		fmt.Sprintf("b/%s (vault)", relPath),
		string(defaultContent),
		string(vaultContent),
	), nil
}

// DiffAll returns unified diffs for all customized templates.
func (r *Registry) DiffAll(vaultTemplatesDir string) string {
	var parts []string
	for _, fs := range r.Compare(vaultTemplatesDir) {
		if fs.Status != StatusCustomized {
			continue
		}
		// File exists and differs — Diff won't error here
		d, _ := r.Diff(vaultTemplatesDir, fs.Entry.RelPath)
		if d != "" {
			parts = append(parts, d)
		}
	}
	return strings.Join(parts, "\n")
}

// unifiedDiff produces a unified diff between two strings with proper hunk headers.
func unifiedDiff(nameA, nameB, a, b string) string {
	linesA := splitLines(a)
	linesB := splitLines(b)

	if slicesEqual(linesA, linesB) {
		return ""
	}

	// Build edit script from LCS
	lcs := computeLCS(linesA, linesB)
	ops := buildOps(linesA, linesB, lcs)

	// Group ops into hunks (3 lines of context)
	hunks := groupHunks(ops, 3)

	var sb strings.Builder
	fmt.Fprintf(&sb, "--- %s\n", nameA)
	fmt.Fprintf(&sb, "+++ %s\n", nameB)

	for _, h := range hunks {
		// Count lines in each side of the hunk
		var aCount, bCount int
		for _, op := range h.ops {
			switch op.kind {
			case opContext:
				aCount++
				bCount++
			case opRemove:
				aCount++
			case opAdd:
				bCount++
			}
		}
		fmt.Fprintf(&sb, "@@ -%d,%d +%d,%d @@\n", h.aStart+1, aCount, h.bStart+1, bCount)
		for _, op := range h.ops {
			switch op.kind {
			case opContext:
				fmt.Fprintf(&sb, " %s\n", op.line)
			case opRemove:
				fmt.Fprintf(&sb, "-%s\n", op.line)
			case opAdd:
				fmt.Fprintf(&sb, "+%s\n", op.line)
			}
		}
	}

	return sb.String()
}

type opKind int

const (
	opContext opKind = iota
	opRemove
	opAdd
)

type diffOp struct {
	kind   opKind
	line   string
	aLine  int // 0-based line number in A
	bLine  int // 0-based line number in B
}

type hunk struct {
	aStart int
	bStart int
	ops    []diffOp
}

// buildOps converts the LCS into a sequence of context/remove/add operations.
func buildOps(a, b, lcs []string) []diffOp {
	var ops []diffOp
	i, j, k := 0, 0, 0
	for i < len(a) || j < len(b) {
		if k < len(lcs) && i < len(a) && j < len(b) && a[i] == lcs[k] && b[j] == lcs[k] {
			ops = append(ops, diffOp{kind: opContext, line: a[i], aLine: i, bLine: j})
			i++
			j++
			k++
		} else {
			for i < len(a) && (k >= len(lcs) || a[i] != lcs[k]) {
				ops = append(ops, diffOp{kind: opRemove, line: a[i], aLine: i})
				i++
			}
			for j < len(b) && (k >= len(lcs) || b[j] != lcs[k]) {
				ops = append(ops, diffOp{kind: opAdd, line: b[j], bLine: j})
				j++
			}
		}
	}
	return ops
}

// groupHunks splits ops into hunks with ctx lines of surrounding context.
// Adjacent changes within 2*ctx lines are merged into one hunk.
func groupHunks(ops []diffOp, ctx int) []hunk {
	// Find ranges of change (non-context) ops
	type changeRange struct{ start, end int }
	var changes []changeRange
	for i, op := range ops {
		if op.kind != opContext {
			if len(changes) > 0 && changes[len(changes)-1].end == i {
				changes[len(changes)-1].end = i + 1
			} else {
				changes = append(changes, changeRange{i, i + 1})
			}
		}
	}

	if len(changes) == 0 {
		return nil
	}

	// Merge change ranges that are within 2*ctx of each other
	merged := []changeRange{changes[0]}
	for _, cr := range changes[1:] {
		last := &merged[len(merged)-1]
		if cr.start-last.end <= 2*ctx {
			last.end = cr.end
		} else {
			merged = append(merged, cr)
		}
	}

	// Expand each merged range with context and build hunks
	var hunks []hunk
	for _, cr := range merged {
		start := cr.start - ctx
		if start < 0 {
			start = 0
		}
		end := cr.end + ctx
		if end > len(ops) {
			end = len(ops)
		}

		h := hunk{ops: ops[start:end]}

		// Determine starting line numbers
		if ops[start].kind == opAdd {
			// Find the A-side line from context before, or 0
			h.aStart = ops[start].bLine // will be corrected below
			h.bStart = ops[start].bLine
			// Walk back to find an aLine
			for s := start; s < end; s++ {
				if ops[s].kind != opAdd {
					h.aStart = ops[s].aLine - (s - start)
					break
				}
			}
		} else {
			h.aStart = ops[start].aLine
			// Find bStart from first op that has a bLine
			h.bStart = 0
			for s := start; s < end; s++ {
				if ops[s].kind != opRemove {
					h.bStart = ops[s].bLine - countKind(ops[start:s], opContext) - countKind(ops[start:s], opAdd)
					break
				}
			}
		}

		hunks = append(hunks, h)
	}

	return hunks
}

func countKind(ops []diffOp, kind opKind) int {
	n := 0
	for _, op := range ops {
		if op.kind == kind {
			n++
		}
	}
	return n
}

// computeLCS returns the longest common subsequence of two string slices.
func computeLCS(a, b []string) []string {
	m, n := len(a), len(b)
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}

	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if a[i-1] == b[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}

	lcs := make([]string, 0, dp[m][n])
	i, j := m, n
	for i > 0 && j > 0 {
		if a[i-1] == b[j-1] {
			lcs = append(lcs, a[i-1])
			i--
			j--
		} else if dp[i-1][j] >= dp[i][j-1] {
			i--
		} else {
			j--
		}
	}
	for left, right := 0, len(lcs)-1; left < right; left, right = left+1, right-1 {
		lcs[left], lcs[right] = lcs[right], lcs[left]
	}
	return lcs
}

// splitLines splits a string into lines, stripping the trailing newline.
func splitLines(s string) []string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
