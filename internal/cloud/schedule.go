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

// ScheduleSyncer handles sync_schedule messages from the cloud.
type ScheduleSyncer struct {
	db    *db.DB
	sched *scheduler.Scheduler
	conn  *Connector
}

// NewScheduleSyncer creates a ScheduleSyncer and registers it as a handler
// on the connector for TypeSyncSchedule messages.
func NewScheduleSyncer(conn *Connector, database *db.DB, sched *scheduler.Scheduler) *ScheduleSyncer {
	ss := &ScheduleSyncer{
		db:    database,
		sched: sched,
		conn:  conn,
	}
	conn.Handle(types.TypeSyncSchedule, ss.handle)
	return ss
}

// handle is the MessageHandler for TypeSyncSchedule messages.
func (ss *ScheduleSyncer) handle(payload json.RawMessage) {
	var schedules []types.ScheduleSync
	if err := json.Unmarshal(payload, &schedules); err != nil {
		slog.Error("cloud: invalid sync_schedule payload", "error", err)
		return
	}

	slog.Info("cloud: schedule sync received", "schedules", len(schedules))

	if err := ss.sync(context.Background(), schedules); err != nil {
		slog.Error("cloud: schedule sync failed", "error", err)
		return
	}

	ss.sendAck()
}

// sync processes the schedule manifest from the cloud.
func (ss *ScheduleSyncer) sync(ctx context.Context, schedules []types.ScheduleSync) error {
	// Build a set of cloud schedule IDs for deletion detection.
	cloudIDs := make(map[string]struct{}, len(schedules))
	for _, s := range schedules {
		cloudIDs[s.ID] = struct{}{}
	}

	// Process each schedule.
	for _, s := range schedules {
		if err := ss.syncSchedule(ctx, s); err != nil {
			slog.Error("cloud: sync schedule failed", "id", s.ID, "error", err)
		}
	}

	// Delete cloud-origin schedules that are no longer in the manifest.
	if err := ss.deleteStale(cloudIDs); err != nil {
		slog.Error("cloud: deleting stale schedules", "error", err)
	}

	// Trigger scheduler reload so it picks up the new schedules.
	ss.sched.TriggerReload()

	return nil
}

// syncSchedule handles a single schedule from the manifest.
func (ss *ScheduleSyncer) syncSchedule(ctx context.Context, s types.ScheduleSync) error {
	// Validate the cron expression before storing.
	if _, err := scheduler.ParseTimeWindow(s.CronExpr); err != nil {
		return fmt.Errorf("invalid cron_expr %q for schedule %s: %w", s.CronExpr, s.ID, err)
	}

	_, err := ss.db.Exec(`
		INSERT INTO schedules (id, playlist_id, cron_expr, priority, origin)
		VALUES (?, ?, ?, ?, 'cloud')
		ON CONFLICT(id) DO UPDATE SET
			playlist_id = excluded.playlist_id,
			cron_expr = excluded.cron_expr,
			priority = excluded.priority,
			origin = 'cloud'`,
		s.ID, s.PlaylistID, s.CronExpr, s.Priority,
	)
	if err != nil {
		return fmt.Errorf("upserting schedule %s: %w", s.ID, err)
	}

	slog.Info("cloud: schedule synced", "id", s.ID, "playlist_id", s.PlaylistID, "cron_expr", s.CronExpr)
	return nil
}

// deleteStale removes cloud-origin schedules that are no longer in the manifest.
// Local schedules (origin = 'local') are always preserved.
func (ss *ScheduleSyncer) deleteStale(cloudIDs map[string]struct{}) error {
	rows, err := ss.db.Query(`SELECT id FROM schedules WHERE origin = 'cloud'`)
	if err != nil {
		return fmt.Errorf("listing cloud schedules: %w", err)
	}

	// Collect stale IDs first, then close rows before executing deletes
	// (SQLite MaxOpenConns=1 — cannot hold rows open and Exec simultaneously).
	var stale []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("scanning cloud schedule row: %w", err)
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
		slog.Info("cloud: removing stale schedule", "id", id)
		if _, err := ss.db.Exec(`DELETE FROM schedules WHERE id = ?`, id); err != nil {
			slog.Error("cloud: deleting stale schedule", "id", id, "error", err)
		}
	}

	return nil
}

// sendAck sends a sync_ack message for schedule sync.
func (ss *ScheduleSyncer) sendAck() {
	ack := types.SyncAck{SyncType: "schedule"}
	payload, err := json.Marshal(ack)
	if err != nil {
		slog.Error("cloud: marshal schedule sync ack", "error", err)
		return
	}

	if err := ss.conn.SendMessage(context.Background(), types.WSMessage{
		Type:    types.TypeSyncAck,
		Payload: payload,
	}); err != nil {
		slog.Warn("cloud: failed to send schedule sync ack", "error", err)
	}
}
