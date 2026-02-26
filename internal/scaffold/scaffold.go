package scaffold

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

//go:embed all:templates
var templates embed.FS

// Options controls scaffold behavior.
type Options struct {
	GitInit bool // run git init after scaffolding
}

// Init creates a new vibe-vault Obsidian vault at targetPath.
func Init(targetPath string, opts Options) error {
	targetPath, err := filepath.Abs(targetPath)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}

	// Refuse if target already contains a vault or vibe-vault state.
	if dirExists(filepath.Join(targetPath, ".obsidian")) {
		return fmt.Errorf("%s already contains .obsidian/ — refusing to overwrite", targetPath)
	}
	if dirExists(filepath.Join(targetPath, ".vibe-vault")) {
		return fmt.Errorf("%s already contains .vibe-vault/ — refusing to overwrite", targetPath)
	}

	vaultName := filepath.Base(targetPath)

	// Walk embedded templates and copy to target.
	err = fs.WalkDir(templates, "templates", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Strip the "templates/" prefix to get the relative path within the vault.
		rel, err := filepath.Rel("templates", path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		dest := filepath.Join(targetPath, rel)

		if d.IsDir() {
			return os.MkdirAll(dest, 0o755)
		}

		data, err := templates.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read embedded %s: %w", path, err)
		}

		// Template substitution for README.md
		if rel == "README.md" {
			data = []byte(strings.ReplaceAll(string(data), "{{VAULT_NAME}}", vaultName))
		}

		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}

		perm := filePermission(rel)
		return os.WriteFile(dest, data, perm)
	})
	if err != nil {
		return fmt.Errorf("scaffold vault: %w", err)
	}

	if opts.GitInit {
		cmd := exec.Command("git", "init", targetPath)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("git init: %w", err)
		}
	}

	return nil
}

// filePermission returns 0o755 for shell scripts and git hooks, 0o644 for everything else.
func filePermission(rel string) os.FileMode {
	if strings.HasSuffix(rel, ".sh") {
		return 0o755
	}
	if strings.HasPrefix(rel, ".githooks/") || strings.HasPrefix(rel, filepath.Join(".githooks")+string(filepath.Separator)) {
		return 0o755
	}
	return 0o644
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
