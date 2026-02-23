package db

import (
	"fmt"
	"log/slog"
)

// migration represents a single, numbered schema change.
type migration struct {
	version     int
	description string
	sql         string
}

// migrations is the ordered list of all schema migrations.
// Append new migrations to the end; never modify or reorder existing entries.
var migrations = []migration{
	{
		version:     1,
		description: "initial schema",
		sql: `
CREATE TABLE content_items (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    type        TEXT NOT NULL CHECK(type IN ('image','video','url','html')),
    source      TEXT NOT NULL,
    duration_s  INTEGER NOT NULL DEFAULT 10,
    size_bytes  INTEGER NOT NULL DEFAULT 0,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE playlists (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    is_default INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE playlist_items (
    id                  TEXT PRIMARY KEY,
    playlist_id         TEXT NOT NULL REFERENCES playlists(id) ON DELETE CASCADE,
    content_id          TEXT NOT NULL REFERENCES content_items(id) ON DELETE CASCADE,
    position            INTEGER NOT NULL,
    duration_override_s INTEGER
);

CREATE TABLE schedules (
    id          TEXT PRIMARY KEY,
    playlist_id TEXT NOT NULL REFERENCES playlists(id) ON DELETE CASCADE,
    cron_expr   TEXT,
    priority    INTEGER NOT NULL DEFAULT 0,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE device_meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE UNIQUE INDEX uq_default_playlist
    ON playlists(is_default) WHERE is_default = 1;
`,
	},
	{
		version:     2,
		description: "add allow_popups to content_items",
		sql:         `ALTER TABLE content_items ADD COLUMN allow_popups INTEGER NOT NULL DEFAULT 0;`,
	},
	{
		version:     3,
		description: "seed default playlist",
		sql: `INSERT INTO playlists (id, name, is_default)
		      SELECT '00000000-0000-0000-0000-000000000001', 'Default', 1
		      WHERE NOT EXISTS (SELECT 1 FROM playlists WHERE is_default = 1);`,
	},
	{
		version:     4,
		description: "add checksum and origin columns for cloud sync",
		sql: `ALTER TABLE content_items ADD COLUMN checksum TEXT NOT NULL DEFAULT '';
ALTER TABLE content_items ADD COLUMN origin TEXT NOT NULL DEFAULT 'local';`,
	},
	{
		version:     5,
		description: "add origin column to playlists for cloud sync",
		sql:         `ALTER TABLE playlists ADD COLUMN origin TEXT NOT NULL DEFAULT 'local';`,
	},
	{
		version:     6,
		description: "add origin column to schedules for cloud sync",
		sql:         `ALTER TABLE schedules ADD COLUMN origin TEXT NOT NULL DEFAULT 'local';`,
	},
}

// migrate applies all pending migrations in order. It creates the
// schema_version table on first run and is safe to call on every startup.
func (db *DB) migrate() error {
	// The schema_version table is managed outside the migration list so it
	// can exist before any numbered migration runs.
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_version (
			version     INTEGER PRIMARY KEY,
			description TEXT    NOT NULL,
			applied_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		return fmt.Errorf("creating schema_version table: %w", err)
	}

	var current int
	row := db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_version")
	if err := row.Scan(&current); err != nil {
		return fmt.Errorf("reading current schema version: %w", err)
	}

	// Backward compatibility: if the tables already exist (created by the
	// old CREATE IF NOT EXISTS approach) but no migrations have been
	// recorded, stamp migration 1 as applied without re-running its SQL.
	if current == 0 {
		if existing, err := db.tableExists("content_items"); err != nil {
			return fmt.Errorf("checking existing schema: %w", err)
		} else if existing {
			slog.Info("detected pre-migration database, stamping migration 1")
			if _, err := db.Exec(
				"INSERT INTO schema_version (version, description) VALUES (?, ?)",
				1, "initial schema (stamped)",
			); err != nil {
				return fmt.Errorf("stamping migration 1: %w", err)
			}
			current = 1
		}
	}

	for _, m := range migrations {
		if m.version <= current {
			continue
		}

		slog.Info("applying migration", "version", m.version, "description", m.description)

		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning transaction for migration %d: %w", m.version, err)
		}

		if _, err := tx.Exec(m.sql); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %d (%s): %w", m.version, m.description, err)
		}

		if _, err := tx.Exec(
			"INSERT INTO schema_version (version, description) VALUES (?, ?)",
			m.version, m.description,
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("recording migration %d (%s): %w", m.version, m.description, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration %d: %w", m.version, err)
		}
	}

	slog.Debug("database schema up to date", "version", migrations[len(migrations)-1].version)
	return nil
}
