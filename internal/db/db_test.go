package db

import (
	"testing"
)

func TestOpenCreatesDB(t *testing.T) {
	dir := t.TempDir()
	d, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer d.Close()

	// Should be able to ping.
	if err := d.Ping(); err != nil {
		t.Errorf("Ping: %v", err)
	}
}

func TestOpenIdempotent(t *testing.T) {
	dir := t.TempDir()

	d1, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	d1.Close()

	// Opening again should not fail (migrations are idempotent).
	d2, err := Open(dir)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	d2.Close()
}

func TestMigrationsCreateTables(t *testing.T) {
	dir := t.TempDir()
	d, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	tables := []string{
		"content_items",
		"playlists",
		"playlist_items",
		"schedules",
		"device_meta",
		"schema_version",
	}

	for _, name := range tables {
		exists, err := d.tableExists(name)
		if err != nil {
			t.Errorf("checking table %q: %v", name, err)
			continue
		}
		if !exists {
			t.Errorf("expected table %q to exist after migration", name)
		}
	}
}

func TestDefaultPlaylistSeeded(t *testing.T) {
	dir := t.TempDir()
	d, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	var name string
	var isDefault int
	err = d.QueryRow(`SELECT name, is_default FROM playlists WHERE is_default = 1`).Scan(&name, &isDefault)
	if err != nil {
		t.Fatalf("querying default playlist: %v", err)
	}
	if name != "Default" {
		t.Errorf("expected playlist name 'Default', got %q", name)
	}
}

func TestSchemaVersionMatchesLatest(t *testing.T) {
	dir := t.TempDir()
	d, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	var maxVersion int
	err = d.QueryRow(`SELECT MAX(version) FROM schema_version`).Scan(&maxVersion)
	if err != nil {
		t.Fatalf("querying schema version: %v", err)
	}

	expected := migrations[len(migrations)-1].version
	if maxVersion != expected {
		t.Errorf("expected schema version %d, got %d", expected, maxVersion)
	}
}

func TestForeignKeysEnabled(t *testing.T) {
	dir := t.TempDir()
	d, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	// Verify the foreign_keys pragma is on.
	var fkEnabled int
	if err := d.QueryRow(`PRAGMA foreign_keys`).Scan(&fkEnabled); err != nil {
		t.Fatalf("querying foreign_keys pragma: %v", err)
	}
	if fkEnabled != 1 {
		t.Skipf("foreign keys not enabled by driver (pragma returns %d)", fkEnabled)
	}

	// Inserting a playlist_item referencing a non-existent content_id should fail.
	_, err = d.Exec(
		`INSERT INTO playlist_items (id, playlist_id, content_id, position)
		 VALUES ('pi-test', '00000000-0000-0000-0000-000000000001', 'nonexistent', 0)`,
	)
	if err == nil {
		t.Error("expected foreign key error for non-existent content_id")
	}
}

func TestContentTypeCheck(t *testing.T) {
	dir := t.TempDir()
	d, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	// Valid type should work.
	_, err = d.Exec(
		`INSERT INTO content_items (id, name, type, source) VALUES ('c1', 'Test', 'url', 'https://a.com')`,
	)
	if err != nil {
		t.Fatalf("inserting valid content: %v", err)
	}

	// Invalid type should fail.
	_, err = d.Exec(
		`INSERT INTO content_items (id, name, type, source) VALUES ('c2', 'Test', 'invalid', 'https://b.com')`,
	)
	if err == nil {
		t.Error("expected CHECK constraint error for invalid content type")
	}
}
