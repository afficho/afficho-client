package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // register sqlite driver
)

// DB wraps sql.DB with afficho-specific helpers.
type DB struct {
	*sql.DB
}

// Open opens (or creates) the SQLite database inside dataDir and runs
// any pending migrations.
func Open(dataDir string) (*DB, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating data dir %q: %w", dataDir, err)
	}

	dbPath := filepath.Join(dataDir, "afficho.db")
	dsn := dbPath + "?_journal=WAL&_timeout=5000&_foreign_keys=on"

	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	// SQLite performs best with a single writer connection.
	sqlDB.SetMaxOpenConns(1)

	db := &DB{sqlDB}
	if err := db.migrate(); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return db, nil
}

// tableExists reports whether a table with the given name exists in the database.
func (db *DB) tableExists(name string) (bool, error) {
	var count int
	err := db.QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", name,
	).Scan(&count)
	return count > 0, err
}
