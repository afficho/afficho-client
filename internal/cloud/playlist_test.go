package cloud

import (
	"context"
	"encoding/json"
	"testing"

	types "github.com/afficho/afficho-types"

	"github.com/afficho/afficho-client/internal/scheduler"
)

func TestPlaylistSyncUpsertsPlaylist(t *testing.T) {
	database, mgr, conn := testSetup(t)
	sched := scheduler.New(database, mgr)
	ps := NewPlaylistSyncer(conn, database, sched)

	// Seed content items.
	_, err := database.Exec(`
		INSERT INTO content_items (id, name, type, source, duration_s, origin)
		VALUES ('c-1', 'Content 1', 'url', 'https://example.com/1', 10, 'cloud'),
		       ('c-2', 'Content 2', 'url', 'https://example.com/2', 20, 'cloud')`)
	if err != nil {
		t.Fatalf("seed content: %v", err)
	}

	playlists := []types.PlaylistSync{
		{
			PlaylistID: "pl-1",
			Name:       "Cloud Playlist",
			Items: []types.PlaylistSyncItem{
				{ContentID: "c-1", Position: 0, DurationS: 15},
				{ContentID: "c-2", Position: 1, DurationS: 25},
			},
		},
	}

	if err := ps.sync(context.Background(), playlists); err != nil {
		t.Fatalf("sync: %v", err)
	}

	// Verify playlist row.
	var name, origin string
	err = database.QueryRow(
		`SELECT name, origin FROM playlists WHERE id = ?`, "pl-1",
	).Scan(&name, &origin)
	if err != nil {
		t.Fatalf("query playlist: %v", err)
	}
	if name != "Cloud Playlist" {
		t.Errorf("expected name 'Cloud Playlist', got %q", name)
	}
	if origin != "cloud" {
		t.Errorf("expected origin 'cloud', got %q", origin)
	}

	// Verify playlist items.
	var count int
	database.QueryRow(`SELECT COUNT(*) FROM playlist_items WHERE playlist_id = 'pl-1'`).Scan(&count)
	if count != 2 {
		t.Errorf("expected 2 playlist items, got %d", count)
	}

	// Verify item ordering and duration override.
	var contentID string
	var position int
	var durationOverride *int
	err = database.QueryRow(
		`SELECT content_id, position, duration_override_s FROM playlist_items WHERE playlist_id = 'pl-1' ORDER BY position ASC LIMIT 1`,
	).Scan(&contentID, &position, &durationOverride)
	if err != nil {
		t.Fatalf("query first item: %v", err)
	}
	if contentID != "c-1" {
		t.Errorf("expected first item content_id 'c-1', got %q", contentID)
	}
	if position != 0 {
		t.Errorf("expected position 0, got %d", position)
	}
	if durationOverride == nil || *durationOverride != 15 {
		t.Errorf("expected duration_override_s 15, got %v", durationOverride)
	}
}

func TestPlaylistSyncReplacesItems(t *testing.T) {
	database, mgr, conn := testSetup(t)
	sched := scheduler.New(database, mgr)
	ps := NewPlaylistSyncer(conn, database, sched)

	// Seed content.
	_, err := database.Exec(`
		INSERT INTO content_items (id, name, type, source, duration_s, origin)
		VALUES ('c-1', 'Content 1', 'url', 'https://example.com/1', 10, 'cloud'),
		       ('c-2', 'Content 2', 'url', 'https://example.com/2', 20, 'cloud')`)
	if err != nil {
		t.Fatalf("seed content: %v", err)
	}

	// First sync: playlist with c-1.
	first := []types.PlaylistSync{
		{
			PlaylistID: "pl-1",
			Name:       "Playlist V1",
			Items: []types.PlaylistSyncItem{
				{ContentID: "c-1", Position: 0, DurationS: 10},
			},
		},
	}
	if err := ps.sync(context.Background(), first); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	// Second sync: same playlist, now with c-2 only.
	second := []types.PlaylistSync{
		{
			PlaylistID: "pl-1",
			Name:       "Playlist V2",
			Items: []types.PlaylistSyncItem{
				{ContentID: "c-2", Position: 0, DurationS: 20},
			},
		},
	}
	if err := ps.sync(context.Background(), second); err != nil {
		t.Fatalf("second sync: %v", err)
	}

	// Verify name was updated.
	var name string
	database.QueryRow(`SELECT name FROM playlists WHERE id = 'pl-1'`).Scan(&name)
	if name != "Playlist V2" {
		t.Errorf("expected name 'Playlist V2', got %q", name)
	}

	// Verify only c-2 is in the playlist now.
	var count int
	database.QueryRow(`SELECT COUNT(*) FROM playlist_items WHERE playlist_id = 'pl-1'`).Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 playlist item after replace, got %d", count)
	}

	var contentID string
	database.QueryRow(`SELECT content_id FROM playlist_items WHERE playlist_id = 'pl-1'`).Scan(&contentID)
	if contentID != "c-2" {
		t.Errorf("expected content_id 'c-2', got %q", contentID)
	}
}

func TestPlaylistSyncDeletesStale(t *testing.T) {
	database, mgr, conn := testSetup(t)
	sched := scheduler.New(database, mgr)
	ps := NewPlaylistSyncer(conn, database, sched)

	// Pre-populate with a cloud playlist that won't be in the new manifest.
	_, err := database.Exec(`
		INSERT INTO playlists (id, name, origin) VALUES ('stale-pl', 'Stale', 'cloud')`)
	if err != nil {
		t.Fatalf("insert stale playlist: %v", err)
	}

	// Also add a local playlist that should NOT be deleted.
	_, err = database.Exec(`
		INSERT INTO playlists (id, name, origin) VALUES ('local-pl', 'Local', 'local')`)
	if err != nil {
		t.Fatalf("insert local playlist: %v", err)
	}

	// Sync with empty manifest — stale cloud playlists should be removed.
	if err := ps.sync(context.Background(), nil); err != nil {
		t.Fatalf("sync: %v", err)
	}

	// stale-pl should be gone.
	var count int
	database.QueryRow(`SELECT COUNT(*) FROM playlists WHERE id = 'stale-pl'`).Scan(&count)
	if count != 0 {
		t.Error("expected stale cloud playlist to be deleted")
	}

	// local-pl should still exist.
	database.QueryRow(`SELECT COUNT(*) FROM playlists WHERE id = 'local-pl'`).Scan(&count)
	if count != 1 {
		t.Error("expected local playlist to be preserved")
	}
}

func TestPlaylistSyncPreservesLocalPlaylists(t *testing.T) {
	database, mgr, conn := testSetup(t)
	sched := scheduler.New(database, mgr)
	ps := NewPlaylistSyncer(conn, database, sched)

	// Seed content.
	_, err := database.Exec(`
		INSERT INTO content_items (id, name, type, source, duration_s, origin)
		VALUES ('c-1', 'Content 1', 'url', 'https://example.com/1', 10, 'local')`)
	if err != nil {
		t.Fatalf("seed content: %v", err)
	}

	// The default playlist (seeded by migration 3) is local. Verify it's not touched.
	var defaultCount int
	database.QueryRow(`SELECT COUNT(*) FROM playlists WHERE is_default = 1`).Scan(&defaultCount)
	if defaultCount != 1 {
		t.Fatalf("expected 1 default playlist, got %d", defaultCount)
	}

	// Sync a cloud playlist.
	playlists := []types.PlaylistSync{
		{
			PlaylistID: "cloud-pl",
			Name:       "Cloud Only",
			Items:      []types.PlaylistSyncItem{{ContentID: "c-1", Position: 0, DurationS: 10}},
		},
	}
	if err := ps.sync(context.Background(), playlists); err != nil {
		t.Fatalf("sync: %v", err)
	}

	// Default playlist should still exist.
	database.QueryRow(`SELECT COUNT(*) FROM playlists WHERE is_default = 1`).Scan(&defaultCount)
	if defaultCount != 1 {
		t.Error("expected default playlist to be preserved after cloud sync")
	}

	// Cloud playlist should exist.
	var cloudCount int
	database.QueryRow(`SELECT COUNT(*) FROM playlists WHERE id = 'cloud-pl'`).Scan(&cloudCount)
	if cloudCount != 1 {
		t.Error("expected cloud playlist to exist")
	}
}

func TestPlaylistSyncHandlerRegistered(t *testing.T) {
	database, mgr, conn := testSetup(t)
	sched := scheduler.New(database, mgr)
	NewPlaylistSyncer(conn, database, sched)

	if _, ok := conn.handlers[types.TypeSyncPlaylist]; !ok {
		t.Fatal("expected sync_playlist handler to be registered")
	}
}

func TestPlaylistSyncMessageDispatch(t *testing.T) {
	database, mgr, conn := testSetup(t)
	sched := scheduler.New(database, mgr)
	_ = NewPlaylistSyncer(conn, database, sched)

	// Seed content.
	_, err := database.Exec(`
		INSERT INTO content_items (id, name, type, source, duration_s, origin)
		VALUES ('c-1', 'Content 1', 'url', 'https://example.com/1', 10, 'cloud')`)
	if err != nil {
		t.Fatalf("seed content: %v", err)
	}

	// Simulate receiving a sync_playlist message.
	playlists := []types.PlaylistSync{
		{
			PlaylistID: "dispatch-pl",
			Name:       "Dispatched",
			Items:      []types.PlaylistSyncItem{{ContentID: "c-1", Position: 0, DurationS: 10}},
		},
	}
	payload, _ := json.Marshal(playlists)

	// Call the handler directly.
	conn.handlers[types.TypeSyncPlaylist](payload)

	// Verify it ended up in the DB.
	var name string
	err = database.QueryRow(
		`SELECT name FROM playlists WHERE id = ?`, "dispatch-pl",
	).Scan(&name)
	if err != nil {
		t.Fatalf("expected playlist in DB: %v", err)
	}
	if name != "Dispatched" {
		t.Errorf("expected name 'Dispatched', got %q", name)
	}
}
