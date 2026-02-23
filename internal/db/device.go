package db

import (
	"database/sql"
	"fmt"

	"github.com/google/uuid"
)

const deviceIDKey = "device_id"

// DeviceID returns the stable device identifier, generating and persisting
// a new UUIDv4 on first call. Subsequent calls return the stored value.
func (db *DB) DeviceID() (string, error) {
	var id string
	err := db.QueryRow(
		"SELECT value FROM device_meta WHERE key = ?", deviceIDKey,
	).Scan(&id)

	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return "", fmt.Errorf("reading device ID: %w", err)
	}

	// First run — generate and store a new ID.
	id = uuid.New().String()
	if _, err := db.Exec(
		"INSERT INTO device_meta (key, value) VALUES (?, ?)", deviceIDKey, id,
	); err != nil {
		return "", fmt.Errorf("storing device ID: %w", err)
	}

	return id, nil
}
