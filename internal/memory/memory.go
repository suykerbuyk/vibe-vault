// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

// Package memory manages the symlink between Claude Code's host-local
// per-project memory directory (~/.claude/projects/{slug}/memory/) and
// the vault-resident auto-memory directory
// (VibeVault/Projects/{name}/agentctx/memory/).
//
// The symlink makes Claude Code's native auto-memory writes land on
// vault disk transparently, so memory is synchronized across machines
// through vault git sync rather than through a sidecar process.
package memory

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/suykerbuyk/vibe-vault/internal/session"
)

// Opts controls a Link or Unlink invocation.
type Opts struct {
	// WorkingDir is the project's absolute working directory. Zero value
	// means "use os.Getwd()". Symlinks are resolved before slug
	// computation.
	WorkingDir string

	// VaultPath is the absolute path to the VibeVault root. Required.
	VaultPath string

	// Force overrides refusal on wrong-symlink or conflicting files.
	Force bool

	// DryRun reports actions without performing any I/O side effects.
	DryRun bool

	// HomeDir overrides the detected home directory (for tests). Zero
	// value means "use os.UserHomeDir()". Production callers leave this
	// empty.
	HomeDir string

	// Now returns the current time; injectable for deterministic
	// conflict-directory naming in tests. Zero value means time.Now.
	Now func() time.Time
}

// Action describes a single side effect applied (or proposed, in
// dry-run) during Link / Unlink. Intended for human-readable reporting.
type Action struct {
	Kind   string // e.g. "CREATE", "SYMLINK", "MOVE", "DROP", "REMOVE", "WARN"
	Path   string
	Detail string
}

// Result summarizes a Link or Unlink run.
type Result struct {
	Project       string   // detected project name
	Slug          string   // computed Claude slug
	SourcePath    string   // ~/.claude/projects/{slug}/memory (the symlink location)
	TargetPath    string   // VibeVault/.../agentctx/memory (the symlink target)
	AlreadyLinked bool     // true when Link was a no-op
	Actions       []Action // ordered list of performed actions
}

// Link establishes ~/.claude/projects/{slug}/memory → VibeVault target.
//
// Migrates any pre-existing host-local memory into the vault target
// before creating the symlink. Idempotent: re-running on an already
// linked project returns success with AlreadyLinked=true.
func Link(opts Opts) (*Result, error) {
	rs, err := resolve(opts)
	if err != nil {
		return nil, err
	}

	res := &Result{
		Project:    rs.project,
		Slug:       rs.slug,
		SourcePath: rs.sourcePath,
		TargetPath: rs.targetPath,
	}

	// Scope check: project must already be vibe-vault tracked.
	if _, err := os.Stat(rs.agentctxDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("agentctx not found for project %q: run `vv init` first to mark this project as vibe-vault-tracked", rs.project)
	} else if err != nil {
		return nil, fmt.Errorf("stat agentctx: %w", err)
	}

	// Ensure target exists.
	if _, err := os.Stat(rs.targetPath); os.IsNotExist(err) {
		if !opts.DryRun {
			if mkErr := os.MkdirAll(rs.targetPath, 0o755); mkErr != nil {
				return nil, fmt.Errorf("create target: %w", mkErr)
			}
		}
		res.Actions = append(res.Actions, Action{Kind: "CREATE", Path: rs.targetPath, Detail: "vault memory dir"})
	} else if err != nil {
		return nil, fmt.Errorf("stat target: %w", err)
	}

	// Ensure host parent exists (fresh machine case where Claude Code
	// has never opened the project).
	parent := filepath.Dir(rs.sourcePath)
	if _, err := os.Stat(parent); os.IsNotExist(err) {
		if !opts.DryRun {
			if mkErr := os.MkdirAll(parent, 0o755); mkErr != nil {
				return nil, fmt.Errorf("create claude project parent: %w", mkErr)
			}
		}
		res.Actions = append(res.Actions, Action{Kind: "CREATE", Path: parent, Detail: "claude project parent"})
	} else if err != nil {
		return nil, fmt.Errorf("stat parent: %w", err)
	}

	// Inspect current state at source.
	info, lerr := os.Lstat(rs.sourcePath)
	switch {
	case lerr != nil && os.IsNotExist(lerr):
		// Nothing at source — straight to symlink creation.
	case lerr != nil:
		return nil, fmt.Errorf("lstat source: %w", lerr)
	case info.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(rs.sourcePath)
		if err != nil {
			return nil, fmt.Errorf("readlink: %w", err)
		}
		if equalPaths(target, rs.targetPath) {
			res.AlreadyLinked = true
			return res, nil
		}
		if !opts.Force {
			return nil, fmt.Errorf("source %s is a symlink pointing to %s; expected %s (re-run with --force to repair)", rs.sourcePath, target, rs.targetPath)
		}
		if !opts.DryRun {
			if err := os.Remove(rs.sourcePath); err != nil {
				return nil, fmt.Errorf("remove wrong symlink: %w", err)
			}
		}
		res.Actions = append(res.Actions, Action{Kind: "REMOVE", Path: rs.sourcePath, Detail: "wrong symlink"})
	default:
		// Real directory — migrate contents.
		if err := migrateDir(rs, opts, res); err != nil {
			return nil, err
		}
	}

	// Create the symlink.
	if !opts.DryRun {
		if err := os.Symlink(rs.targetPath, rs.sourcePath); err != nil {
			return nil, fmt.Errorf("create symlink: %w", err)
		}
	}
	res.Actions = append(res.Actions, Action{Kind: "SYMLINK", Path: rs.sourcePath, Detail: "→ " + rs.targetPath})

	return res, nil
}

// Unlink reverses Link: removes the symlink, restores a real
// directory, and copies files from the vault target into it. The vault
// copy is preserved as the durable store.
func Unlink(opts Opts) (*Result, error) {
	rs, err := resolve(opts)
	if err != nil {
		return nil, err
	}

	res := &Result{
		Project:    rs.project,
		Slug:       rs.slug,
		SourcePath: rs.sourcePath,
		TargetPath: rs.targetPath,
	}

	info, lerr := os.Lstat(rs.sourcePath)
	if lerr != nil {
		if os.IsNotExist(lerr) {
			return nil, fmt.Errorf("not linked, nothing to undo: %s does not exist", rs.sourcePath)
		}
		return nil, fmt.Errorf("lstat source: %w", lerr)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return nil, fmt.Errorf("not linked, nothing to undo: %s is a real directory", rs.sourcePath)
	}

	target, err := os.Readlink(rs.sourcePath)
	if err != nil {
		return nil, fmt.Errorf("readlink: %w", err)
	}
	if !equalPaths(target, rs.targetPath) && !opts.Force {
		return nil, fmt.Errorf("symlink points to %s, not expected %s (re-run with --force to detach anyway)", target, rs.targetPath)
	}

	if !opts.DryRun {
		if err := os.Remove(rs.sourcePath); err != nil {
			return nil, fmt.Errorf("remove symlink: %w", err)
		}
		if err := os.MkdirAll(rs.sourcePath, 0o755); err != nil {
			return nil, fmt.Errorf("recreate dir: %w", err)
		}
	}
	res.Actions = append(res.Actions,
		Action{Kind: "REMOVE", Path: rs.sourcePath, Detail: "symlink"},
		Action{Kind: "CREATE", Path: rs.sourcePath, Detail: "real directory"},
	)

	// Copy files from vault target to the new real directory. Vault
	// retains the original.
	if _, err := os.Stat(rs.targetPath); err == nil {
		files, err := os.ReadDir(rs.targetPath)
		if err != nil {
			return nil, fmt.Errorf("read target: %w", err)
		}
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			src := filepath.Join(rs.targetPath, f.Name())
			dst := filepath.Join(rs.sourcePath, f.Name())
			if !opts.DryRun {
				if err := copyFile(src, dst); err != nil {
					return nil, fmt.Errorf("copy %s: %w", f.Name(), err)
				}
			}
			res.Actions = append(res.Actions, Action{Kind: "COPY", Path: dst, Detail: "from vault"})
		}
	}

	return res, nil
}

// resolved bundles derived paths used by Link and Unlink.
type resolved struct {
	cwd          string
	project      string
	slug         string
	homeDir      string
	sourcePath   string // ~/.claude/projects/{slug}/memory
	agentctxDir  string // {vault}/Projects/{name}/agentctx
	targetPath   string // {vault}/Projects/{name}/agentctx/memory
	conflictRoot string // {vault}/Projects/{name}/agentctx/memory-conflicts
	now          time.Time
}

func resolve(opts Opts) (*resolved, error) {
	if opts.VaultPath == "" {
		return nil, fmt.Errorf("vault path is required")
	}

	cwd := opts.WorkingDir
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("getwd: %w", err)
		}
	}

	abs, err := filepath.Abs(cwd)
	if err != nil {
		return nil, fmt.Errorf("abs cwd: %w", err)
	}
	// Resolve symlinks so symlinked cwds produce a single canonical
	// slug. Fall back to the cleaned absolute path when the evaluation
	// fails (e.g. temp dir eviction during a test).
	if evald, symErr := filepath.EvalSymlinks(abs); symErr == nil {
		abs = evald
	}
	abs = filepath.Clean(abs)

	slug := slugFromPath(abs)

	project := session.DetectProject(abs)
	if project == "" || project == "_unknown" {
		return nil, fmt.Errorf("could not detect project for %s", abs)
	}

	home := opts.HomeDir
	if home == "" {
		h, homeErr := os.UserHomeDir()
		if homeErr != nil {
			return nil, fmt.Errorf("user home: %w", homeErr)
		}
		home = h
	}

	vault, err := filepath.Abs(opts.VaultPath)
	if err != nil {
		return nil, fmt.Errorf("abs vault: %w", err)
	}

	agentctxDir := filepath.Join(vault, "Projects", project, "agentctx")
	targetPath := filepath.Join(agentctxDir, "memory")
	conflictRoot := filepath.Join(agentctxDir, "memory-conflicts")
	sourcePath := filepath.Join(home, ".claude", "projects", slug, "memory")

	now := time.Now
	if opts.Now != nil {
		now = opts.Now
	}

	return &resolved{
		cwd:          abs,
		project:      project,
		slug:         slug,
		homeDir:      home,
		sourcePath:   sourcePath,
		agentctxDir:  agentctxDir,
		targetPath:   targetPath,
		conflictRoot: conflictRoot,
		now:          now(),
	}, nil
}

// slugFromPath derives Claude Code's per-project slug from an absolute
// working-directory path by replacing every "/" with "-". The leading
// slash becomes a leading dash. Trailing slashes are stripped before
// conversion.
func slugFromPath(abs string) string {
	abs = strings.TrimRight(abs, "/")
	if abs == "" {
		abs = "/"
	}
	return strings.ReplaceAll(abs, "/", "-")
}

// migrateDir walks the existing host-local memory dir and relocates
// each file into the vault target, then removes the empty origin.
func migrateDir(rs *resolved, opts Opts, res *Result) error {
	files, err := os.ReadDir(rs.sourcePath)
	if err != nil {
		return fmt.Errorf("read source dir: %w", err)
	}

	if len(files) == 0 {
		if !opts.DryRun {
			if err := os.Remove(rs.sourcePath); err != nil {
				return fmt.Errorf("remove empty dir: %w", err)
			}
		}
		res.Actions = append(res.Actions, Action{Kind: "REMOVE", Path: rs.sourcePath, Detail: "empty directory"})
		return nil
	}

	// First pass: classify every file and detect unresolvable
	// conflicts before mutating anything. This keeps the operation
	// closer to transactional when --force is absent.
	type plan struct {
		name   string
		src    string
		dst    string
		action string // "MOVE", "DROP", "CONFLICT"
	}

	var plans []plan
	// Deterministic iteration order for reproducible action logs.
	names := make([]string, 0, len(files))
	for _, f := range files {
		if f.IsDir() {
			// Nested directories are unexpected; surface them so a
			// human can triage. Refuse rather than silently drop.
			return fmt.Errorf("unexpected subdirectory in memory: %s (manual cleanup required)", filepath.Join(rs.sourcePath, f.Name()))
		}
		names = append(names, f.Name())
	}
	sort.Strings(names)

	var unresolved []string
	for _, name := range names {
		src := filepath.Join(rs.sourcePath, name)
		dst := filepath.Join(rs.targetPath, name)

		targetInfo, err := os.Stat(dst)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("stat target %s: %w", dst, err)
		}
		p := plan{name: name, src: src, dst: dst}
		switch {
		case err != nil: // not exist
			p.action = "MOVE"
		case targetInfo.IsDir():
			return fmt.Errorf("target %s is a directory, cannot overwrite", dst)
		default:
			same, err := sameContent(src, dst)
			if err != nil {
				return fmt.Errorf("compare %s: %w", name, err)
			}
			if same {
				p.action = "DROP"
			} else {
				p.action = "CONFLICT"
				if !opts.Force {
					unresolved = append(unresolved, name)
				}
			}
		}
		plans = append(plans, p)
	}

	if len(unresolved) > 0 {
		return fmt.Errorf("conflicting files in host-local memory differ from vault copy: %s (re-run with --force to quarantine host-local versions in memory-conflicts/)", strings.Join(unresolved, ", "))
	}

	// Execute plans.
	var conflictDir string
	for _, p := range plans {
		switch p.action {
		case "MOVE":
			if !opts.DryRun {
				if err := moveFile(p.src, p.dst); err != nil {
					return fmt.Errorf("move %s: %w", p.name, err)
				}
			}
			res.Actions = append(res.Actions, Action{Kind: "MOVE", Path: p.dst, Detail: "from host-local"})
		case "DROP":
			if !opts.DryRun {
				if err := os.Remove(p.src); err != nil {
					return fmt.Errorf("drop %s: %w", p.name, err)
				}
			}
			res.Actions = append(res.Actions, Action{Kind: "DROP", Path: p.src, Detail: "identical to vault copy"})
		case "CONFLICT":
			if conflictDir == "" {
				conflictDir = filepath.Join(rs.conflictRoot, rs.now.UTC().Format("20060102T150405Z"))
				if !opts.DryRun {
					if err := os.MkdirAll(conflictDir, 0o755); err != nil {
						return fmt.Errorf("create conflict dir: %w", err)
					}
				}
				res.Actions = append(res.Actions, Action{Kind: "CREATE", Path: conflictDir, Detail: "conflict dir"})
			}
			quarantine := filepath.Join(conflictDir, p.name)
			if !opts.DryRun {
				if err := moveFile(p.src, quarantine); err != nil {
					return fmt.Errorf("quarantine %s: %w", p.name, err)
				}
			}
			res.Actions = append(res.Actions,
				Action{Kind: "MOVE", Path: quarantine, Detail: "quarantined host-local"},
				Action{Kind: "WARN", Path: p.name, Detail: "content differs from vault copy"},
			)
		}
	}

	// Remove the now-empty source directory.
	if !opts.DryRun {
		// Directory should be empty after DROP/MOVE operations.
		if err := os.Remove(rs.sourcePath); err != nil {
			return fmt.Errorf("remove source dir: %w", err)
		}
	}
	res.Actions = append(res.Actions, Action{Kind: "REMOVE", Path: rs.sourcePath, Detail: "migrated directory"})
	return nil
}

// equalPaths reports whether two paths refer to the same file location
// after cleaning. Both must be absolute or will be compared literally.
func equalPaths(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b)
}

// sameContent returns true when two files have identical SHA-256
// digests. Hashing is cheaper than byte-by-byte comparison for the
// small-file, repeat-compare pattern we use here and keeps the code
// simple.
func sameContent(a, b string) (bool, error) {
	ha, err := hashFile(a)
	if err != nil {
		return false, err
	}
	hb, err := hashFile(b)
	if err != nil {
		return false, err
	}
	return ha == hb, nil
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if mkErr := os.MkdirAll(filepath.Dir(dst), 0o755); mkErr != nil {
		return mkErr
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, copyErr := io.Copy(out, in); copyErr != nil {
		return copyErr
	}
	info, err := os.Stat(src)
	if err == nil {
		_ = os.Chmod(dst, info.Mode())
	}
	return nil
}

// moveFile moves src to dst, preferring os.Rename and falling back to
// copy+remove across devices (the vault and HOME may sit on different
// filesystems).
func moveFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := copyFile(src, dst); err != nil {
		return err
	}
	return os.Remove(src)
}
