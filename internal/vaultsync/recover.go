// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package vaultsync

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// RecoveryCandidate describes an upstream commit reachable from HEAD
// whose recorded blob for at least one Manual-class file differs from
// HEAD's current blob for the same file. These are the candidates an
// operator can inspect with `vv vault recover --show <sha>` after a
// previous rebase resolution dropped the upstream-side content in favor
// of local work.
type RecoveryCandidate struct {
	SHA         string
	Subject     string
	Author      string
	CommittedAt time.Time
	Files       []string // vault-relative paths the commit touched whose blob differs from HEAD's
}

// Recover lists upstream commits whose Manual-class file content was
// dropped by prior rebase resolutions. It walks REACHABLE history from
// HEAD back days days, identifying commits where the recorded blob for
// a Manual-class file differs from HEAD's content. Reflog is NOT
// consulted — after a peer machine's rebase pushes to the remote, that
// machine's prior commits remain reachable from main; the "drop"
// happened to file content during merge resolution, not to commit
// reachability.
//
// days <= 0 returns an empty slice (no commits in zero/negative window)
// without erroring.
//
// The returned slice is sorted by CommittedAt descending.
func Recover(vaultPath string, days int) ([]RecoveryCandidate, error) {
	if days <= 0 {
		return nil, nil
	}

	since := time.Now().AddDate(0, 0, -days)
	// Format: <sha>\0<subject>\0<author>\0<iso8601>\n then file paths,
	// one per line, then a blank line separator.
	out, err := gitCmd(vaultPath, 30*time.Second,
		"log",
		"--since="+since.Format(time.RFC3339),
		"--name-only",
		"--format=%H%x00%s%x00%an%x00%cI",
	)
	if err != nil {
		return nil, fmt.Errorf("git log: %w", err)
	}
	if out == "" {
		return nil, nil
	}

	// HEAD blob may be missing for paths the commit touched but which
	// no longer exist at HEAD. Treat missing-at-HEAD as "differs" so
	// the operator sees a candidate they may want to restore.
	headBlobs := map[string]string{}
	headBlob := func(path string) (string, bool) {
		if b, ok := headBlobs[path]; ok {
			return b, b != ""
		}
		blob, err := gitCmd(vaultPath, 10*time.Second, "show", "HEAD:"+path)
		if err != nil {
			headBlobs[path] = ""
			return "", false
		}
		headBlobs[path] = blob
		return blob, true
	}

	var candidates []RecoveryCandidate

	// Parse: --format=<NUL-delimited>%n then a blank line, then
	// --name-only file paths one per line, then directly the next
	// commit's header. Walk line-by-line; the NUL byte in a line
	// uniquely identifies a header so we don't have to count blanks
	// precisely.
	lines := strings.Split(out, "\n")
	i := 0
	for i < len(lines) {
		header := lines[i]
		i++
		if !strings.Contains(header, "\x00") {
			// Blank or unrecognized; skip until the next header.
			continue
		}
		fields := strings.SplitN(header, "\x00", 4)
		if len(fields) != 4 {
			continue
		}
		sha := strings.TrimSpace(fields[0])
		subject := fields[1]
		author := fields[2]
		committedAt, _ := time.Parse(time.RFC3339, strings.TrimSpace(fields[3]))

		var diffFiles []string
		// Walk file-name lines until the next header (line containing
		// NUL) or end of input. Blank lines are separators only and
		// contribute no path.
		for i < len(lines) {
			peek := lines[i]
			if strings.Contains(peek, "\x00") {
				break
			}
			i++
			path := strings.TrimSpace(peek)
			if path == "" {
				continue
			}
			if Classify(path) != Manual {
				continue
			}
			commitBlob, err := gitCmd(vaultPath, 10*time.Second, "show", sha+":"+path)
			if err != nil {
				// File didn't exist at this commit (e.g., merge
				// metadata or rename). Skip silently.
				continue
			}
			hb, exists := headBlob(path)
			if !exists {
				// HEAD has no such path; commit's content is a
				// candidate to restore.
				diffFiles = append(diffFiles, path)
				continue
			}
			if commitBlob != hb {
				diffFiles = append(diffFiles, path)
			}
		}

		if len(diffFiles) > 0 {
			candidates = append(candidates, RecoveryCandidate{
				SHA:         sha,
				Subject:     subject,
				Author:      author,
				CommittedAt: committedAt,
				Files:       diffFiles,
			})
		}
	}

	sort.Slice(candidates, func(a, b int) bool {
		return candidates[a].CommittedAt.After(candidates[b].CommittedAt)
	})

	return candidates, nil
}
