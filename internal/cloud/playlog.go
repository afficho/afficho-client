package cloud

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	types "github.com/afficho/afficho-types"
	"github.com/google/uuid"

	"github.com/afficho/afficho-client/internal/db"
)

const (
	// Default flush interval for sending proof-of-play records.
	defaultFlushInterval = 60 * time.Second
	// Default maximum batch size per flush.
	defaultBatchSize = 50
)

// playRecord wraps the shared ProofOfPlayRecord with a local database ID
// used for tracking sync state. The ID is not sent over the wire.
type playRecord struct {
	id  string // local SQLite primary key
	rec types.ProofOfPlayRecord
}

// PlayLogger records content item transitions and periodically flushes
// proof-of-play records to the cloud.
type PlayLogger struct {
	db   *db.DB
	conn *Connector

	flushInterval time.Duration
	batchSize     int

	mu        sync.Mutex
	currentID string
	startedAt time.Time
}

// NewPlayLogger creates a PlayLogger. Call Run to start the background
// flush loop. Hook RecordTransition into the scheduler's OnChange callback.
func NewPlayLogger(conn *Connector, database *db.DB) *PlayLogger {
	return &PlayLogger{
		db:            database,
		conn:          conn,
		flushInterval: defaultFlushInterval,
		batchSize:     defaultBatchSize,
	}
}

// RecordTransition records the end of the previous item's playback and
// begins tracking the new item. Call this whenever the displayed content
// changes. Pass the new content item ID (empty string if nothing is playing).
func (pl *PlayLogger) RecordTransition(newItemID string) {
	pl.mu.Lock()
	defer pl.mu.Unlock()

	now := time.Now().UTC()

	// Finalize the previous item's play record.
	if pl.currentID != "" && !pl.startedAt.IsZero() {
		duration := int(now.Sub(pl.startedAt).Seconds())
		if duration > 0 {
			pl.insertRecord(pl.currentID, pl.startedAt, duration)
		}
	}

	// Start tracking the new item.
	pl.currentID = newItemID
	if newItemID != "" {
		pl.startedAt = now
	} else {
		pl.startedAt = time.Time{}
	}
}

// insertRecord writes a single play record to the local database.
// Must be called with pl.mu held.
func (pl *PlayLogger) insertRecord(contentID string, startedAt time.Time, durationS int) {
	id := uuid.New().String()
	_, err := pl.db.Exec(`
		INSERT INTO proof_of_play (id, content_id, started_at, duration_s)
		VALUES (?, ?, ?, ?)`,
		id, contentID, startedAt.Format(time.RFC3339), durationS,
	)
	if err != nil {
		slog.Error("playlog: failed to insert record", "content_id", contentID, "error", err)
		return
	}
	slog.Debug("playlog: recorded", "content_id", contentID, "duration_s", durationS)
}

// Run starts the background flush loop. It periodically sends unsynced
// proof-of-play records to the cloud. Blocks until ctx is cancelled.
func (pl *PlayLogger) Run(ctx context.Context) {
	ticker := time.NewTicker(pl.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Final flush on shutdown.
			pl.flush()
			return
		case <-ticker.C:
			pl.flush()
		}
	}
}

// PendingCount returns the number of unsynced proof-of-play records.
func (pl *PlayLogger) PendingCount() int {
	var count int
	if err := pl.db.QueryRow(`SELECT COUNT(*) FROM proof_of_play WHERE synced = 0`).Scan(&count); err != nil {
		slog.Error("playlog: count pending", "error", err)
		return 0
	}
	return count
}

// flush sends a batch of unsynced records to the cloud and marks them synced.
func (pl *PlayLogger) flush() {
	locals, err := pl.loadUnsynced()
	if err != nil {
		slog.Error("playlog: loading unsynced records", "error", err)
		return
	}
	if len(locals) == 0 {
		return
	}

	// Convert to wire-format records (no local ID).
	wire := make([]types.ProofOfPlayRecord, len(locals))
	for i, l := range locals {
		wire[i] = l.rec
	}

	payload, err := json.Marshal(wire)
	if err != nil {
		slog.Error("playlog: marshal records", "error", err)
		return
	}

	if err := pl.conn.SendMessage(context.Background(), types.WSMessage{
		Type:    types.TypeProofOfPlay,
		Payload: payload,
	}); err != nil {
		slog.Debug("playlog: send failed (will retry)", "error", err, "records", len(locals))
		return
	}

	// Mark as synced.
	pl.markSynced(locals)
	slog.Info("playlog: flushed records to cloud", "count", len(locals))
}

// loadUnsynced reads up to batchSize unsynced records from the database.
func (pl *PlayLogger) loadUnsynced() ([]playRecord, error) {
	rows, err := pl.db.Query(`
		SELECT id, content_id, started_at, duration_s
		FROM proof_of_play
		WHERE synced = 0
		ORDER BY started_at ASC
		LIMIT ?`, pl.batchSize)
	if err != nil {
		return nil, err
	}

	var records []playRecord
	for rows.Next() {
		var r playRecord
		if err := rows.Scan(&r.id, &r.rec.ContentID, &r.rec.StartedAt, &r.rec.DurationS); err != nil {
			rows.Close()
			return nil, err
		}
		records = append(records, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	return records, nil
}

// markSynced sets the synced flag on the given records.
func (pl *PlayLogger) markSynced(records []playRecord) {
	for _, r := range records {
		if _, err := pl.db.Exec(`UPDATE proof_of_play SET synced = 1 WHERE id = ?`, r.id); err != nil {
			slog.Error("playlog: marking synced", "id", r.id, "error", err)
		}
	}
}
