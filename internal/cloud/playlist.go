package cloud

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	types "github.com/afficho/afficho-types"

	"github.com/afficho/afficho-client/internal/db"
	"github.com/afficho/afficho-client/internal/scheduler"
)

// PlaylistSyncer handles sync_playlist messages from the cloud.
type PlaylistSyncer struct {
	db    *db.DB
	sched *scheduler.Scheduler
	conn  *Connector
}

// NewPlaylistSyncer creates a PlaylistSyncer and registers it as a handler
// on the connector for TypeSyncPlaylist messages.
func NewPlaylistSyncer(conn *Connector, database *db.DB, sched *scheduler.Scheduler) *PlaylistSyncer {
	ps := &PlaylistSyncer{
		db:    database,
		sched: sched,
		conn:  conn,
	}
	conn.Handle(types.TypeSyncPlaylist, ps.handle)
	return ps
}

// handle is the MessageHandler for TypeSyncPlaylist messages.
func (ps *PlaylistSyncer) handle(payload json.RawMessage) {
	var playlists []types.PlaylistSync
	if err := json.Unmarshal(payload, &playlists); err != nil {
		slog.Error("cloud: invalid sync_playlist payload", "error", err)
		return
	}

	slog.Info("cloud: playlist sync received", "playlists", len(playlists))

	if err := ps.sync(context.Background(), playlists); err != nil {
		slog.Error("cloud: playlist sync failed", "error", err)
		return
	}

	ps.sendAck()
}

// sync processes the playlist manifest from the cloud.
func (ps *PlaylistSyncer) sync(ctx context.Context, playlists []types.PlaylistSync) error {
	// Build a set of cloud playlist IDs for deletion detection.
	cloudIDs := make(map[string]struct{}, len(playlists))
	for _, pl := range playlists {
		cloudIDs[pl.PlaylistID] = struct{}{}
	}

	// Process each playlist.
	for _, pl := range playlists {
		if err := ps.syncPlaylist(ctx, pl); err != nil {
			slog.Error("cloud: sync playlist failed", "id", pl.PlaylistID, "name", pl.Name, "error", err)
		}
	}

	// Delete cloud-origin playlists that are no longer in the manifest.
	if err := ps.deleteStale(cloudIDs); err != nil {
		slog.Error("cloud: deleting stale playlists", "error", err)
	}

	// Trigger scheduler reload so it picks up the new playlists.
	ps.sched.TriggerReload()

	return nil
}

// syncPlaylist handles a single playlist from the manifest.
func (ps *PlaylistSyncer) syncPlaylist(ctx context.Context, pl types.PlaylistSync) error {
	tx, err := ps.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // commit or rollback

	// Upsert the playlist row.
	_, err = tx.Exec(`
		INSERT INTO playlists (id, name, origin)
		VALUES (?, ?, 'cloud')
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			origin = 'cloud'`,
		pl.PlaylistID, pl.Name,
	)
	if err != nil {
		return fmt.Errorf("upserting playlist %s: %w", pl.PlaylistID, err)
	}

	// Replace all playlist items: delete existing, insert new.
	_, err = tx.Exec(`DELETE FROM playlist_items WHERE playlist_id = ?`, pl.PlaylistID)
	if err != nil {
		return fmt.Errorf("clearing playlist items for %s: %w", pl.PlaylistID, err)
	}

	for _, item := range pl.Items {
		var durationOverride *int
		if item.DurationS > 0 {
			d := item.DurationS
			durationOverride = &d
		}

		_, err = tx.Exec(`
			INSERT INTO playlist_items (id, playlist_id, content_id, position, duration_override_s)
			VALUES (?, ?, ?, ?, ?)`,
			fmt.Sprintf("%s-%d", pl.PlaylistID, item.Position),
			pl.PlaylistID, item.ContentID, item.Position, durationOverride,
		)
		if err != nil {
			return fmt.Errorf("inserting playlist item %s pos %d: %w", item.ContentID, item.Position, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing playlist %s: %w", pl.PlaylistID, err)
	}

	slog.Info("cloud: playlist synced", "id", pl.PlaylistID, "name", pl.Name, "items", len(pl.Items))
	return nil
}

// deleteStale removes cloud-origin playlists that are no longer in the manifest.
// Local playlists (origin = 'local') are always preserved.
func (ps *PlaylistSyncer) deleteStale(cloudIDs map[string]struct{}) error {
	rows, err := ps.db.Query(`SELECT id FROM playlists WHERE origin = 'cloud'`)
	if err != nil {
		return fmt.Errorf("listing cloud playlists: %w", err)
	}

	// Collect stale IDs first, then close rows before executing deletes
	// (SQLite MaxOpenConns=1 — cannot hold rows open and Exec simultaneously).
	var stale []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("scanning cloud playlist row: %w", err)
		}
		if _, keep := cloudIDs[id]; !keep {
			stale = append(stale, id)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	for _, id := range stale {
		slog.Info("cloud: removing stale playlist", "id", id)
		// CASCADE will also delete playlist_items.
		if _, err := ps.db.Exec(`DELETE FROM playlists WHERE id = ?`, id); err != nil {
			slog.Error("cloud: deleting stale playlist", "id", id, "error", err)
		}
	}

	return nil
}

// sendAck sends a sync_ack message for playlist sync.
func (ps *PlaylistSyncer) sendAck() {
	ack := types.SyncAck{SyncType: "playlist"}
	payload, err := json.Marshal(ack)
	if err != nil {
		slog.Error("cloud: marshal playlist sync ack", "error", err)
		return
	}

	if err := ps.conn.SendMessage(context.Background(), types.WSMessage{
		Type:    types.TypeSyncAck,
		Payload: payload,
	}); err != nil {
		slog.Warn("cloud: failed to send playlist sync ack", "error", err)
	}
}
