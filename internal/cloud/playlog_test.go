package cloud

import (
	"testing"
	"time"

	"github.com/afficho/afficho-client/internal/config"
)

func TestPlayLogRecordTransition(t *testing.T) {
	database, _, conn := testSetup(t)
	pl := NewPlayLogger(conn, database)

	// Start tracking item "a".
	pl.RecordTransition("a")

	// Simulate some display time.
	pl.mu.Lock()
	pl.startedAt = time.Now().Add(-15 * time.Second)
	pl.mu.Unlock()

	// Transition to item "b" — should finalize "a".
	pl.RecordTransition("b")

	// Verify a record was inserted for "a".
	var count int
	database.QueryRow(`SELECT COUNT(*) FROM proof_of_play WHERE content_id = 'a'`).Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 record for item 'a', got %d", count)
	}

	var durationS int
	database.QueryRow(`SELECT duration_s FROM proof_of_play WHERE content_id = 'a'`).Scan(&durationS)
	if durationS < 14 || durationS > 16 {
		t.Errorf("expected duration ~15s, got %d", durationS)
	}
}

func TestPlayLogSkipsZeroDuration(t *testing.T) {
	database, _, conn := testSetup(t)
	pl := NewPlayLogger(conn, database)

	// Start and immediately transition — duration ~0s should not create a record.
	pl.RecordTransition("a")
	pl.RecordTransition("b")

	var count int
	database.QueryRow(`SELECT COUNT(*) FROM proof_of_play WHERE content_id = 'a'`).Scan(&count)
	if count != 0 {
		t.Errorf("expected no record for zero-duration play, got %d", count)
	}
}

func TestPlayLogTransitionToEmpty(t *testing.T) {
	database, _, conn := testSetup(t)
	pl := NewPlayLogger(conn, database)

	// Start tracking.
	pl.RecordTransition("a")
	pl.mu.Lock()
	pl.startedAt = time.Now().Add(-10 * time.Second)
	pl.mu.Unlock()

	// Transition to empty (nothing playing) — should finalize "a".
	pl.RecordTransition("")

	var count int
	database.QueryRow(`SELECT COUNT(*) FROM proof_of_play WHERE content_id = 'a'`).Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 record for item 'a', got %d", count)
	}
}

func TestPlayLogPendingCount(t *testing.T) {
	database, _, conn := testSetup(t)
	pl := NewPlayLogger(conn, database)

	if pl.PendingCount() != 0 {
		t.Errorf("expected 0 pending, got %d", pl.PendingCount())
	}

	// Insert two records.
	pl.RecordTransition("a")
	pl.mu.Lock()
	pl.startedAt = time.Now().Add(-5 * time.Second)
	pl.mu.Unlock()
	pl.RecordTransition("b")
	pl.mu.Lock()
	pl.startedAt = time.Now().Add(-5 * time.Second)
	pl.mu.Unlock()
	pl.RecordTransition("")

	if pl.PendingCount() != 2 {
		t.Errorf("expected 2 pending, got %d", pl.PendingCount())
	}
}

func TestPlayLogFlushMarksRecordsSynced(t *testing.T) {
	database, _, conn := testSetup(t)
	pl := NewPlayLogger(conn, database)

	// Insert a record.
	pl.RecordTransition("a")
	pl.mu.Lock()
	pl.startedAt = time.Now().Add(-10 * time.Second)
	pl.mu.Unlock()
	pl.RecordTransition("")

	if pl.PendingCount() != 1 {
		t.Fatalf("expected 1 pending, got %d", pl.PendingCount())
	}

	// Flush will fail to send (not connected) — records stay unsynced.
	pl.flush()
	if pl.PendingCount() != 1 {
		t.Errorf("expected 1 still pending after failed flush, got %d", pl.PendingCount())
	}
}

func TestPlayLogLoadUnsynced(t *testing.T) {
	database, _, conn := testSetup(t)
	pl := NewPlayLogger(conn, database)

	// Insert multiple records.
	for _, id := range []string{"x", "y", "z"} {
		pl.RecordTransition(id)
		pl.mu.Lock()
		pl.startedAt = time.Now().Add(-5 * time.Second)
		pl.mu.Unlock()
	}
	pl.RecordTransition("")

	records, err := pl.loadUnsynced()
	if err != nil {
		t.Fatalf("loadUnsynced: %v", err)
	}
	if len(records) != 3 {
		t.Errorf("expected 3 unsynced records, got %d", len(records))
	}
}

func TestPlayLogBatchSizeLimit(t *testing.T) {
	database, _, _ := testSetup(t)
	conn := New(config.CloudConfig{}, "test-device", "dev", t.TempDir())
	pl := NewPlayLogger(conn, database)
	pl.batchSize = 2

	// Insert 3 records.
	for _, id := range []string{"a", "b", "c"} {
		pl.RecordTransition(id)
		pl.mu.Lock()
		pl.startedAt = time.Now().Add(-5 * time.Second)
		pl.mu.Unlock()
	}
	pl.RecordTransition("")

	records, err := pl.loadUnsynced()
	if err != nil {
		t.Fatalf("loadUnsynced: %v", err)
	}
	if len(records) != 2 {
		t.Errorf("expected batch size limit of 2, got %d", len(records))
	}
}
