// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package flowdoc

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// This file is the direction-independent repo-introspection core for
// `vv flowdoc gen` (see the flowdoc-gen-source-ingestion task). WalkRepo
// enumerates and filters a project's source files; RepoView's ReadFile
// and Search are the lazy content accessors. The single-shot generator
// consumes this as a prompt-builder; an agentic generator would wire the
// same accessors as its tool backend — neither path needs new core code.

const (
	// maxFileBytes caps a single RepoView.ReadFile content read. It
	// matches the 1 MiB vault-accessor convention. Oversize files are
	// still enumerated in RepoView.Files (the tree listing stays honest)
	// but their content is refused — the prompt-builder lists them,
	// never inlines them.
	maxFileBytes = 1 << 20

	// gitCmdTimeout bounds every git invocation WalkRepo makes. It is
	// generous next to session.gitRemoteProject's 1s budget because
	// `git ls-files` over a large checkout is heavier than a single
	// `git remote get-url`.
	gitCmdTimeout = 10 * time.Second

	// tokenBytesPerToken is the labelled byte/char-to-token heuristic.
	// There is no tokenizer in this repo (flowdoc-gen-source-ingestion
	// M2 explicitly rules one out); Budget.EstimatedTokens is an
	// estimate for budget gating, not a real token count.
	tokenBytesPerToken = 4

	// gitlinkMode is the `git ls-files --stage` mode for a submodule
	// gitlink entry. The path it names is a commit pointer, not a
	// readable file, so WalkRepo drops it (filter class 2).
	gitlinkMode = "160000"
)

// WalkSource records which enumeration path WalkRepo took.
type WalkSource string

const (
	// WalkSourceGit — enumerated via `git ls-files` (the normal path).
	WalkSourceGit WalkSource = "git-ls-files"
	// WalkSourceWalkDir — enumerated via filepath.WalkDir (non-git root).
	WalkSourceWalkDir WalkSource = "walkdir-fallback"
)

// RepoFile is one kept source file: its repo-relative, slash-separated
// path and byte size from os.Stat. Content is never held here — it is
// fetched lazily by RepoView.ReadFile.
type RepoFile struct {
	Path string
	Size int64
}

// Budget is the byte/char accounting for a RepoView. EstimatedTokens is
// a labelled ~4-char/token heuristic (see tokenBytesPerToken), not a
// tokenizer result.
type Budget struct {
	FileCount       int
	TotalBytes      int64
	EstimatedTokens int64
}

// RepoView is the result of WalkRepo: an enumerated, filtered listing of
// a project's source files plus lazy content and search accessors. It is
// the introspection core shared by every flowdoc generator strategy.
type RepoView struct {
	// Root is the absolute repo root. For a git checkout it is the
	// toplevel reported by `git rev-parse --show-toplevel`; for the
	// fallback it is the cleaned absolute form of WalkRepo's argument.
	Root string
	// Files are the kept files, sorted by Path.
	Files []RepoFile
	// Budget is the size accounting over Files.
	Budget Budget
	// Source records which enumeration path produced this view.
	Source WalkSource

	// kept is a path->size index over Files for O(1) membership checks
	// in ReadFile. It is unexported; Files is the public listing.
	kept map[string]int64
}

// SearchHit is one line matched by RepoView.Search.
type SearchHit struct {
	Path string
	Line int
	Text string
}

// WalkRepo enumerates the source files of the project rooted at root. It
// stats but never reads file content — RepoView.ReadFile is the lazy
// content accessor.
//
// Enumeration prefers `git ls-files` (free .gitignore semantics and
// submodule-content exclusion); a root that is not a git checkout falls
// back to filepath.WalkDir. Three filter classes are applied in order:
//
//  1. gitignored      — free on the git path (ignored files never appear
//     in ls-files output); not applied by the WalkDir fallback.
//  2. gitlink entries — 160000-mode submodule paths, dropped; git path only.
//  3. committed-noise — vendor/build/asset directory conventions; both paths.
func WalkRepo(root string) (RepoView, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return RepoView{}, fmt.Errorf("flowdoc: resolve root %q: %w", root, err)
	}
	if info, err := os.Stat(abs); err != nil || !info.IsDir() {
		return RepoView{}, fmt.Errorf("flowdoc: repo root %q is not a directory", abs)
	}

	if top, ok := gitToplevel(abs); ok {
		return walkGit(top)
	}
	return walkFallback(abs)
}

// gitToplevel runs `git -C dir rev-parse --show-toplevel`. On success it
// returns the absolute repo root and true; for a non-git directory — or
// any git failure or timeout — it returns "", false, which is WalkRepo's
// signal to fall back to a plain filesystem walk.
func gitToplevel(dir string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), gitCmdTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	top := strings.TrimSpace(string(out))
	if top == "" {
		return "", false
	}
	return top, true
}

// walkGit enumerates a git checkout via `git ls-files --stage -z`. The
// --stage flag yields the file mode (needed for gitlink detection); -z
// makes the output NUL-delimited so paths with spaces or other special
// characters are not git-quoted. All three filter classes apply:
// gitignored files never appear in the output (class 1, free), 160000
// gitlink entries are dropped (class 2), and isNoisePath drops committed
// vendor/build/asset trees (class 3).
func walkGit(root string) (RepoView, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitCmdTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", root, "ls-files", "--stage", "-z")
	out, err := cmd.Output()
	if err != nil {
		return RepoView{}, fmt.Errorf("flowdoc: git ls-files in %q: %w", root, err)
	}

	var rels []string
	for entry := range strings.SplitSeq(string(out), "\x00") {
		if entry == "" {
			continue
		}
		mode, rel, ok := parseLsFilesEntry(entry)
		if !ok {
			continue
		}
		if mode == gitlinkMode { // class 2: submodule gitlink, not a readable file
			continue
		}
		if isNoisePath(rel) { // class 3: committed-but-noise
			continue
		}
		rels = append(rels, rel)
	}
	return buildView(root, rels, WalkSourceGit)
}

// parseLsFilesEntry splits one `git ls-files --stage -z` record. The
// record format is "<mode> <object> <stage>\t<path>": space-separated
// metadata, then a single tab, then the path. A record with no tab or an
// empty path yields ok=false and is skipped by the caller.
func parseLsFilesEntry(entry string) (mode, path string, ok bool) {
	tab := strings.IndexByte(entry, '\t')
	if tab < 0 {
		return "", "", false
	}
	fields := strings.Fields(entry[:tab])
	path = entry[tab+1:]
	if len(fields) == 0 || path == "" {
		return "", "", false
	}
	return fields[0], path, true
}

// walkFallback is the non-git enumeration path, used when root is not a
// git checkout. Only filter class 3 (committed-noise) applies here —
// gitignore semantics (class 1) and gitlink detection (class 2) require
// git and are meaningless without it. A .git directory is skipped
// defensively even though gitToplevel already failed for this root.
func walkFallback(root string) (RepoView, error) {
	var rels []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if isNoisePath(rel) {
			return nil
		}
		rels = append(rels, rel)
		return nil
	})
	if err != nil {
		return RepoView{}, fmt.Errorf("flowdoc: walk %q: %w", root, err)
	}
	return buildView(root, rels, WalkSourceWalkDir)
}

// buildView stats each enumerated path and assembles the sorted RepoView
// plus its Budget. A path that vanished between enumeration and stat, or
// that is not a regular file (a leftover gitlink directory, a fifo, a
// deleted-but-staged entry), is dropped silently — it has no content to
// offer the generator. Paths are stat-ed but never read here.
func buildView(root string, rels []string, source WalkSource) (RepoView, error) {
	kept := make(map[string]int64, len(rels))
	files := make([]RepoFile, 0, len(rels))
	var totalBytes int64
	for _, rel := range rels {
		info, err := os.Stat(filepath.Join(root, rel))
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		size := info.Size()
		kept[rel] = size
		files = append(files, RepoFile{Path: rel, Size: size})
		totalBytes += size
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return RepoView{
		Root:   root,
		Files:  files,
		Source: source,
		kept:   kept,
		Budget: Budget{
			FileCount:       len(files),
			TotalBytes:      totalBytes,
			EstimatedTokens: totalBytes / tokenBytesPerToken,
		},
	}, nil
}

// noiseSegments is the committed-but-noise denylist (filter class 3): a
// maintained heuristic of directory names that are routinely committed
// to a repo yet carry no project-design signal. recmeet — the reference
// large repo for this work — commits vendor/, dist/, assets/ and share/,
// none of which describe its workflows. This is a heuristic by design:
// tune the set as new project shapes surface. A real source directory
// that happens to be named "build" or "target" would be wrongly dropped;
// that trade is accepted for v1.
var noiseSegments = map[string]struct{}{
	"vendor":       {},
	"third_party":  {},
	"node_modules": {},
	"dist":         {},
	"build":        {},
	"out":          {},
	"target":       {},
	"assets":       {},
	"share":        {},
}

// isNoisePath reports whether rel has any path segment in the
// committed-noise denylist. rel must be slash-separated.
func isNoisePath(rel string) bool {
	for seg := range strings.SplitSeq(rel, "/") {
		if _, ok := noiseSegments[seg]; ok {
			return true
		}
	}
	return false
}

// ReadFile returns the content of a kept file. rel must name a path
// present in the view (one of Files) — an unknown path is rejected
// rather than read, so an agentic tool backend cannot escape the
// filtered set. A file larger than maxFileBytes is enumerated in Files
// but its content is refused: the prompt-builder lists such files,
// it never inlines them.
func (v RepoView) ReadFile(rel string) ([]byte, error) {
	rel = filepath.ToSlash(rel)
	size, ok := v.kept[rel]
	if !ok {
		return nil, fmt.Errorf("flowdoc: %q is not in the repo view", rel)
	}
	if size > maxFileBytes {
		return nil, fmt.Errorf("flowdoc: %q is %d bytes, over the %d-byte cap", rel, size, int64(maxFileBytes))
	}
	data, err := os.ReadFile(filepath.Join(v.Root, rel))
	if err != nil {
		return nil, fmt.Errorf("flowdoc: read %q: %w", rel, err)
	}
	return data, nil
}

// Search scans the content of every kept file for pattern (a Go regexp)
// and returns one SearchHit per matching line. It is the grep backend
// for an agentic generator and a discovery aid for the prompt-builder.
//
// The scan reads each kept file in turn — O(total repo bytes) — which is
// acceptable at the scale this targets (recmeet, the reference large
// repo, is ~2.7 MB of source text). Oversize files (over maxFileBytes)
// are skipped, the same boundary ReadFile enforces; unreadable files are
// skipped silently. Only an invalid pattern is a hard error.
func (v RepoView) Search(pattern string) ([]SearchHit, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("flowdoc: invalid search pattern: %w", err)
	}
	var hits []SearchHit
	for _, f := range v.Files {
		if f.Size > maxFileBytes {
			continue
		}
		lines, ok := readLines(filepath.Join(v.Root, f.Path))
		if !ok {
			continue
		}
		for i, line := range lines {
			if re.MatchString(line) {
				hits = append(hits, SearchHit{Path: f.Path, Line: i + 1, Text: line})
			}
		}
	}
	return hits, nil
}
