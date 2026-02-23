package cloud

import (
	"context"
	"encoding/json"
	"testing"

	types "github.com/afficho/afficho-types"

	"github.com/afficho/afficho-client/internal/scheduler"
)

func TestScheduleSyncUpsertsSchedule(t *testing.T) {
	database, mgr, conn := testSetup(t)
	sched := scheduler.New(database, mgr)
	ss := NewScheduleSyncer(conn, database, sched)

	// Seed a playlist so the FK constraint is satisfied.
	_, err := database.Exec(`INSERT INTO playlists (id, name, origin) VALUES ('pl-1', 'Test Playlist', 'cloud')`)
	if err != nil {
		t.Fatalf("seed playlist: %v", err)
	}

	schedules := []types.ScheduleSync{
		{
			ID:         "sched-1",
			PlaylistID: "pl-1",
			CronExpr:   "08:00-18:00 weekdays",
			Priority:   10,
		},
	}

	if err := ss.sync(context.Background(), schedules); err != nil {
		t.Fatalf("sync: %v", err)
	}

	// Verify schedule row.
	var playlistID, cronExpr, origin string
	var priority int
	err = database.QueryRow(
		`SELECT playlist_id, cron_expr, priority, origin FROM schedules WHERE id = ?`, "sched-1",
	).Scan(&playlistID, &cronExpr, &priority, &origin)
	if err != nil {
		t.Fatalf("query schedule: %v", err)
	}
	if playlistID != "pl-1" {
		t.Errorf("expected playlist_id 'pl-1', got %q", playlistID)
	}
	if cronExpr != "08:00-18:00 weekdays" {
		t.Errorf("expected cron_expr '08:00-18:00 weekdays', got %q", cronExpr)
	}
	if priority != 10 {
		t.Errorf("expected priority 10, got %d", priority)
	}
	if origin != "cloud" {
		t.Errorf("expected origin 'cloud', got %q", origin)
	}
}

func TestScheduleSyncUpdatesExisting(t *testing.T) {
	database, mgr, conn := testSetup(t)
	sched := scheduler.New(database, mgr)
	ss := NewScheduleSyncer(conn, database, sched)

	// Seed playlists.
	_, err := database.Exec(`
		INSERT INTO playlists (id, name, origin) VALUES ('pl-1', 'Playlist 1', 'cloud');
		INSERT INTO playlists (id, name, origin) VALUES ('pl-2', 'Playlist 2', 'cloud')`)
	if err != nil {
		t.Fatalf("seed playlists: %v", err)
	}

	// First sync.
	first := []types.ScheduleSync{
		{ID: "sched-1", PlaylistID: "pl-1", CronExpr: "08:00-18:00 weekdays", Priority: 5},
	}
	if err := ss.sync(context.Background(), first); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	// Second sync: update the schedule to point at a different playlist.
	second := []types.ScheduleSync{
		{ID: "sched-1", PlaylistID: "pl-2", CronExpr: "09:00-17:00 everyday", Priority: 20},
	}
	if err := ss.sync(context.Background(), second); err != nil {
		t.Fatalf("second sync: %v", err)
	}

	var playlistID, cronExpr string
	var priority int
	if err := database.QueryRow(`SELECT playlist_id, cron_expr, priority FROM schedules WHERE id = 'sched-1'`).Scan(&playlistID, &cronExpr, &priority); err != nil {
		t.Fatalf("query schedule: %v", err)
	}
	if playlistID != "pl-2" {
		t.Errorf("expected playlist_id 'pl-2', got %q", playlistID)
	}
	if cronExpr != "09:00-17:00 everyday" {
		t.Errorf("expected updated cron_expr, got %q", cronExpr)
	}
	if priority != 20 {
		t.Errorf("expected priority 20, got %d", priority)
	}
}

func TestScheduleSyncDeletesStale(t *testing.T) {
	database, mgr, conn := testSetup(t)
	sched := scheduler.New(database, mgr)
	ss := NewScheduleSyncer(conn, database, sched)

	// Seed a playlist and a stale cloud schedule.
	_, err := database.Exec(`INSERT INTO playlists (id, name, origin) VALUES ('pl-1', 'Test', 'cloud')`)
	if err != nil {
		t.Fatalf("seed playlist: %v", err)
	}
	_, err = database.Exec(`
		INSERT INTO schedules (id, playlist_id, cron_expr, priority, origin)
		VALUES ('stale-sched', 'pl-1', '08:00-18:00 weekdays', 5, 'cloud')`)
	if err != nil {
		t.Fatalf("insert stale schedule: %v", err)
	}

	// Also add a local schedule that should NOT be deleted.
	_, err = database.Exec(`
		INSERT INTO schedules (id, playlist_id, cron_expr, priority, origin)
		VALUES ('local-sched', 'pl-1', '22:00-06:00 weekends', 1, 'local')`)
	if err != nil {
		t.Fatalf("insert local schedule: %v", err)
	}

	// Sync with empty manifest — stale cloud schedules should be removed.
	if err := ss.sync(context.Background(), nil); err != nil {
		t.Fatalf("sync: %v", err)
	}

	// stale-sched should be gone.
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM schedules WHERE id = 'stale-sched'`).Scan(&count); err != nil {
		t.Fatalf("query stale: %v", err)
	}
	if count != 0 {
		t.Error("expected stale cloud schedule to be deleted")
	}

	// local-sched should still exist.
	if err := database.QueryRow(`SELECT COUNT(*) FROM schedules WHERE id = 'local-sched'`).Scan(&count); err != nil {
		t.Fatalf("query local: %v", err)
	}
	if count != 1 {
		t.Error("expected local schedule to be preserved")
	}
}

func TestScheduleSyncRejectsInvalidCronExpr(t *testing.T) {
	database, mgr, conn := testSetup(t)
	sched := scheduler.New(database, mgr)
	ss := NewScheduleSyncer(conn, database, sched)

	_, err := database.Exec(`INSERT INTO playlists (id, name, origin) VALUES ('pl-1', 'Test', 'cloud')`)
	if err != nil {
		t.Fatalf("seed playlist: %v", err)
	}

	schedules := []types.ScheduleSync{
		{ID: "bad-sched", PlaylistID: "pl-1", CronExpr: "not-a-valid-expr", Priority: 1},
	}

	// Sync should not fail entirely (logs error, continues).
	_ = ss.sync(context.Background(), schedules)

	// Schedule should NOT be in the DB since the cron expression was invalid.
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM schedules WHERE id = 'bad-sched'`).Scan(&count); err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 0 {
		t.Error("expected schedule with invalid cron_expr to not be inserted")
	}
}

func TestScheduleSyncHandlerRegistered(t *testing.T) {
	database, mgr, conn := testSetup(t)
	sched := scheduler.New(database, mgr)
	NewScheduleSyncer(conn, database, sched)

	if _, ok := conn.handlers[types.TypeSyncSchedule]; !ok {
		t.Fatal("expected sync_schedule handler to be registered")
	}
}

func TestScheduleSyncMessageDispatch(t *testing.T) {
	database, mgr, conn := testSetup(t)
	sched := scheduler.New(database, mgr)
	_ = NewScheduleSyncer(conn, database, sched)

	// Seed a playlist.
	_, err := database.Exec(`INSERT INTO playlists (id, name, origin) VALUES ('pl-1', 'Test', 'cloud')`)
	if err != nil {
		t.Fatalf("seed playlist: %v", err)
	}

	// Simulate receiving a sync_schedule message.
	schedules := []types.ScheduleSync{
		{ID: "dispatch-sched", PlaylistID: "pl-1", CronExpr: "09:00-17:00 everyday", Priority: 5},
	}
	payload, _ := json.Marshal(schedules)

	// Call the handler directly.
	conn.handlers[types.TypeSyncSchedule](payload)

	// Verify it ended up in the DB.
	var cronExpr string
	err = database.QueryRow(
		`SELECT cron_expr FROM schedules WHERE id = ?`, "dispatch-sched",
	).Scan(&cronExpr)
	if err != nil {
		t.Fatalf("expected schedule in DB: %v", err)
	}
	if cronExpr != "09:00-17:00 everyday" {
		t.Errorf("expected cron_expr '09:00-17:00 everyday', got %q", cronExpr)
	}
}
