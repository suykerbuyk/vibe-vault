package context

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadVersion_Missing(t *testing.T) {
	dir := t.TempDir()
	vf, err := ReadVersion(dir)
	if err != nil {
		t.Fatalf("ReadVersion: %v", err)
	}
	if vf.SchemaVersion != 0 {
		t.Errorf("SchemaVersion = %d, want 0", vf.SchemaVersion)
	}
}

func TestReadVersion_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	original := VersionFile{
		SchemaVersion: 2,
		CreatedBy:     "vv test",
		CreatedAt:     "2026-01-01T00:00:00Z",
		UpdatedBy:     "vv test",
		UpdatedAt:     "2026-01-01T00:00:00Z",
	}
	if err := WriteVersion(dir, original); err != nil {
		t.Fatalf("WriteVersion: %v", err)
	}

	got, err := ReadVersion(dir)
	if err != nil {
		t.Fatalf("ReadVersion: %v", err)
	}
	if got.SchemaVersion != original.SchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", got.SchemaVersion, original.SchemaVersion)
	}
	if got.CreatedBy != original.CreatedBy {
		t.Errorf("CreatedBy = %q, want %q", got.CreatedBy, original.CreatedBy)
	}
	if got.UpdatedAt != original.UpdatedAt {
		t.Errorf("UpdatedAt = %q, want %q", got.UpdatedAt, original.UpdatedAt)
	}
}

func TestReadVersion_Invalid(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".version"), []byte("not valid toml {{{}"), 0o644)

	_, err := ReadVersion(dir)
	if err == nil {
		t.Fatal("expected error for invalid TOML")
	}
}

func TestWriteVersion_Creates(t *testing.T) {
	dir := t.TempDir()
	vf := VersionFile{SchemaVersion: 1, CreatedBy: "test"}
	if err := WriteVersion(dir, vf); err != nil {
		t.Fatalf("WriteVersion: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, ".version")); os.IsNotExist(err) {
		t.Error(".version file not created")
	}
}

func TestWriteVersion_Overwrites(t *testing.T) {
	dir := t.TempDir()
	vf1 := VersionFile{SchemaVersion: 1, CreatedBy: "v1"}
	if err := WriteVersion(dir, vf1); err != nil {
		t.Fatalf("WriteVersion v1: %v", err)
	}

	vf2 := VersionFile{SchemaVersion: 2, CreatedBy: "v2"}
	if err := WriteVersion(dir, vf2); err != nil {
		t.Fatalf("WriteVersion v2: %v", err)
	}

	got, err := ReadVersion(dir)
	if err != nil {
		t.Fatalf("ReadVersion: %v", err)
	}
	if got.SchemaVersion != 2 {
		t.Errorf("SchemaVersion = %d, want 2", got.SchemaVersion)
	}
	if got.CreatedBy != "v2" {
		t.Errorf("CreatedBy = %q, want %q", got.CreatedBy, "v2")
	}
}

func TestLatestSchemaVersionIsTen(t *testing.T) {
	if LatestSchemaVersion != 10 {
		t.Errorf("LatestSchemaVersion = %d, want 10", LatestSchemaVersion)
	}
}

func TestMigrationsFrom_Zero(t *testing.T) {
	m := migrationsFrom(0)
	if len(m) != 10 {
		t.Errorf("migrationsFrom(0) = %d migrations, want 10", len(m))
	}
	for i, want := range []struct{ from, to int }{
		{0, 1}, {1, 2}, {2, 3}, {3, 4}, {4, 5},
		{5, 6}, {6, 7}, {7, 8}, {8, 9}, {9, 10},
	} {
		if m[i].From != want.from || m[i].To != want.to {
			t.Errorf("migration %d: %d→%d, want %d→%d", i, m[i].From, m[i].To, want.from, want.to)
		}
	}
}

func TestMigrationsFrom_One(t *testing.T) {
	m := migrationsFrom(1)
	if len(m) != 9 {
		t.Errorf("migrationsFrom(1) = %d migrations, want 9", len(m))
	}
	if m[0].From != 1 || m[0].To != 2 {
		t.Errorf("first migration: %d→%d, want 1→2", m[0].From, m[0].To)
	}
	if m[1].From != 2 || m[1].To != 3 {
		t.Errorf("second migration: %d→%d, want 2→3", m[1].From, m[1].To)
	}
}

func TestMigrationsFrom_Two(t *testing.T) {
	m := migrationsFrom(2)
	if len(m) != 8 {
		t.Errorf("migrationsFrom(2) = %d migrations, want 8", len(m))
	}
	if m[0].From != 2 || m[0].To != 3 {
		t.Errorf("first migration: %d→%d, want 2→3", m[0].From, m[0].To)
	}
}

func TestMigrationsFrom_Three(t *testing.T) {
	m := migrationsFrom(3)
	if len(m) != 7 {
		t.Errorf("migrationsFrom(3) = %d migrations, want 7", len(m))
	}
	if m[0].From != 3 || m[0].To != 4 {
		t.Errorf("migration: %d→%d, want 3→4", m[0].From, m[0].To)
	}
}

func TestMigrationsFrom_Four(t *testing.T) {
	m := migrationsFrom(4)
	if len(m) != 6 {
		t.Errorf("migrationsFrom(4) = %d migrations, want 6", len(m))
	}
	if m[0].From != 4 || m[0].To != 5 {
		t.Errorf("migration: %d→%d, want 4→5", m[0].From, m[0].To)
	}
}

func TestMigrationsFrom_Five(t *testing.T) {
	m := migrationsFrom(5)
	if len(m) != 5 {
		t.Errorf("migrationsFrom(5) = %d migrations, want 5", len(m))
	}
	if m[0].From != 5 || m[0].To != 6 {
		t.Errorf("migration: %d→%d, want 5→6", m[0].From, m[0].To)
	}
}

func TestMigrationsFrom_Six(t *testing.T) {
	m := migrationsFrom(6)
	if len(m) != 4 {
		t.Errorf("migrationsFrom(6) = %d migrations, want 4", len(m))
	}
	if m[0].From != 6 || m[0].To != 7 {
		t.Errorf("migration: %d→%d, want 6→7", m[0].From, m[0].To)
	}
}

func TestMigrationsFrom_Seven(t *testing.T) {
	m := migrationsFrom(7)
	if len(m) != 3 {
		t.Errorf("migrationsFrom(7) = %d migrations, want 3", len(m))
	}
	if m[0].From != 7 || m[0].To != 8 {
		t.Errorf("migration: %d→%d, want 7→8", m[0].From, m[0].To)
	}
}

func TestMigrationsFrom_Eight(t *testing.T) {
	m := migrationsFrom(8)
	if len(m) != 2 {
		t.Errorf("migrationsFrom(8) = %d migrations, want 2", len(m))
	}
	if m[0].From != 8 || m[0].To != 9 {
		t.Errorf("migration: %d→%d, want 8→9", m[0].From, m[0].To)
	}
}

func TestMigrationsFrom_Nine(t *testing.T) {
	m := migrationsFrom(9)
	if len(m) != 1 {
		t.Errorf("migrationsFrom(9) = %d migrations, want 1", len(m))
	}
	if m[0].From != 9 || m[0].To != 10 {
		t.Errorf("migration: %d→%d, want 9→10", m[0].From, m[0].To)
	}
}

func TestMigrate9to10IsNoop(t *testing.T) {
	ctx := MigrationContext{AgentctxPath: t.TempDir()}
	actions, err := migrate9to10(ctx)
	if err != nil {
		t.Fatalf("migrate9to10: %v", err)
	}
	if len(actions) != 0 {
		t.Errorf("migrate9to10 returned %d actions, want 0", len(actions))
	}
}

func TestMigrationsFrom_Latest(t *testing.T) {
	m := migrationsFrom(LatestSchemaVersion)
	if len(m) != 0 {
		t.Errorf("migrationsFrom(%d) = %d migrations, want 0", LatestSchemaVersion, len(m))
	}
}
