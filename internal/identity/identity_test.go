// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package identity

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_FullFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".vibe-vault.toml"), []byte(`
[project]
name = "vibe-vault"
domain = "developer-tools"
tags = ["go", "cli", "obsidian", "mcp"]

[meta]
author = "John Suykerbuyk"
company = "syketech"
`), 0o644)

	id, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if id == nil {
		t.Fatal("expected non-nil identity")
	}
	if id.Project.Name != "vibe-vault" {
		t.Errorf("Project.Name = %q, want vibe-vault", id.Project.Name)
	}
	if id.Project.Domain != "developer-tools" {
		t.Errorf("Project.Domain = %q, want developer-tools", id.Project.Domain)
	}
	if len(id.Project.Tags) != 4 {
		t.Errorf("Project.Tags = %v, want 4 tags", id.Project.Tags)
	}
	if id.Meta.Author != "John Suykerbuyk" {
		t.Errorf("Meta.Author = %q", id.Meta.Author)
	}
	if id.Meta.Company != "syketech" {
		t.Errorf("Meta.Company = %q", id.Meta.Company)
	}
}

func TestLoad_MinimalFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".vibe-vault.toml"), []byte(`
[project]
name = "myproj"
`), 0o644)

	id, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if id == nil {
		t.Fatal("expected non-nil identity")
	}
	if id.Project.Name != "myproj" {
		t.Errorf("Project.Name = %q, want myproj", id.Project.Name)
	}
	if id.Project.Domain != "" {
		t.Errorf("Project.Domain = %q, want empty", id.Project.Domain)
	}
}

func TestLoad_MissingFile(t *testing.T) {
	dir := t.TempDir()

	id, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if id != nil {
		t.Errorf("expected nil for missing file, got %+v", id)
	}
}

func TestLoad_MalformedTOML(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".vibe-vault.toml"), []byte(`[project
name = broken`), 0o644)

	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected error for malformed TOML")
	}
}

func TestTemplate_AllCommented(t *testing.T) {
	dir := t.TempDir()
	content := Template("myproj")
	os.WriteFile(filepath.Join(dir, ".vibe-vault.toml"), []byte(content), 0o644)

	// All-commented template should return nil (heuristics take over)
	id, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if id != nil {
		t.Errorf("expected nil for all-commented template, got %+v", id)
	}
}

func TestTemplate_Uncommented(t *testing.T) {
	dir := t.TempDir()
	// Simulate user uncommenting the name line
	os.WriteFile(filepath.Join(dir, ".vibe-vault.toml"), []byte(`
[project]
name = "my-real-project"
# domain = "work"
`), 0o644)

	id, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if id == nil {
		t.Fatal("expected non-nil identity")
	}
	if id.Project.Name != "my-real-project" {
		t.Errorf("Project.Name = %q, want my-real-project", id.Project.Name)
	}
	if id.Project.Domain != "" {
		t.Errorf("Project.Domain = %q, want empty (still commented)", id.Project.Domain)
	}
}

func TestLoad_EmptyName(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".vibe-vault.toml"), []byte(`
[project]
name = ""
domain = "tools"
`), 0o644)

	id, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if id != nil {
		t.Errorf("expected nil for empty name, got %+v", id)
	}
}
