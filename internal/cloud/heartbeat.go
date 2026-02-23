package cloud

import (
	"context"
	"encoding/json"
	"log/slog"
	"math"
	"time"

	types "github.com/afficho/afficho-types"
)

// StatusProvider supplies dynamic device status for heartbeats.
// This decouples the heartbeat from the scheduler package.
type StatusProvider interface {
	// CurrentItemID returns the ID of the currently displayed content item.
	// Returns empty string if nothing is playing.
	CurrentItemID() string
	// ActivePlaylistID returns the ID of the active playlist, or empty.
	ActivePlaylistID() string
	// ScreenOn reports whether the display is currently powered on.
	ScreenOn() bool
}

// HeartbeatConfig holds the parameters for the heartbeat loop.
type HeartbeatConfig struct {
	Interval time.Duration
	DataDir  string // path for disk usage stats
}

// startHeartbeat runs a ticker that sends heartbeats to the cloud at the
// configured interval. It blocks until ctx is cancelled.
func (c *Connector) startHeartbeat(ctx context.Context) {
	interval := time.Duration(c.cfg.HeartbeatInterval) * time.Second
	if interval <= 0 {
		interval = 30 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := c.sendHeartbeat(ctx); err != nil {
				slog.Warn("cloud: heartbeat send failed", "error", err)
			}
		}
	}
}

// sendHeartbeat constructs and sends a single heartbeat message.
func (c *Connector) sendHeartbeat(ctx context.Context) error {
	hb := types.Heartbeat{
		DeviceID:    c.deviceID,
		UptimeS:     int64(math.Round(time.Since(c.startedAt).Seconds())),
		CPUTempC:    cpuTemp(),
		MemUsedPct:  memUsedPct(),
		DiskUsedPct: diskUsedPct(c.dataDir),
		StorageUsed: storageUsedBytes(c.dataDir),
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}

	if c.status != nil {
		hb.CurrentItemID = c.status.CurrentItemID()
		hb.PlaylistID = c.status.ActivePlaylistID()
		hb.ScreenOn = c.status.ScreenOn()
	}

	payload, err := json.Marshal(hb)
	if err != nil {
		return err
	}

	return c.SendMessage(ctx, types.WSMessage{
		Type:    types.TypeHeartbeat,
		Payload: payload,
	})
}
