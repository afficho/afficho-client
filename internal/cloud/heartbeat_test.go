package cloud

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	types "github.com/afficho/afficho-types"
	"github.com/coder/websocket"

	"github.com/afficho/afficho-client/internal/config"
)

// mockStatus implements StatusProvider for testing.
type mockStatus struct {
	itemID     string
	playlistID string
	screenOn   bool
}

func (m *mockStatus) CurrentItemID() string    { return m.itemID }
func (m *mockStatus) ActivePlaylistID() string { return m.playlistID }
func (m *mockStatus) ScreenOn() bool           { return m.screenOn }

func TestHeartbeatSentPeriodically(t *testing.T) {
	var heartbeatCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")

		for {
			_, data, err := conn.Read(r.Context())
			if err != nil {
				return
			}

			var msg types.WSMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}

			if msg.Type == types.TypeHeartbeat {
				heartbeatCount.Add(1)

				// Send ack back.
				ack, _ := json.Marshal(types.WSMessage{Type: types.TypeHeartbeatAck})
				ctx, cancel := context.WithTimeout(r.Context(), time.Second)
				conn.Write(ctx, websocket.MessageText, ack)
				cancel()
			}
		}
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")

	c := New(config.CloudConfig{
		Enabled:           true,
		Endpoint:          wsURL,
		DeviceKey:         "key",
		HeartbeatInterval: 1, // 1 second for fast test
		ReconnectMaxDelay: 1,
	}, "dev-hb", "dev", t.TempDir())

	c.SetStatusProvider(&mockStatus{
		itemID:     "content-1",
		playlistID: "playlist-1",
		screenOn:   true,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	go c.Run(ctx)

	// Wait for at least 2 heartbeats.
	deadline := time.After(4 * time.Second)
	for heartbeatCount.Load() < 2 {
		select {
		case <-deadline:
			t.Fatalf("expected at least 2 heartbeats, got %d", heartbeatCount.Load())
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}
}

func TestHeartbeatContainsDeviceInfo(t *testing.T) {
	var hbPayload atomic.Value

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")

		for {
			_, data, err := conn.Read(r.Context())
			if err != nil {
				return
			}

			var msg types.WSMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}

			if msg.Type == types.TypeHeartbeat {
				hbPayload.Store(msg.Payload)
			}
		}
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")

	c := New(config.CloudConfig{
		Enabled:           true,
		Endpoint:          wsURL,
		DeviceKey:         "key",
		HeartbeatInterval: 1,
		ReconnectMaxDelay: 1,
	}, "device-99", "dev", t.TempDir())

	c.SetStatusProvider(&mockStatus{
		itemID:     "item-42",
		playlistID: "pl-7",
		screenOn:   true,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go c.Run(ctx)

	// Wait for a heartbeat.
	deadline := time.After(3 * time.Second)
	for hbPayload.Load() == nil {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for heartbeat")
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}

	raw := hbPayload.Load().(json.RawMessage)
	var hb types.Heartbeat
	if err := json.Unmarshal(raw, &hb); err != nil {
		t.Fatalf("unmarshal heartbeat: %v", err)
	}

	if hb.DeviceID != "device-99" {
		t.Errorf("expected device ID device-99, got %q", hb.DeviceID)
	}
	if hb.CurrentItemID != "item-42" {
		t.Errorf("expected current item item-42, got %q", hb.CurrentItemID)
	}
	if hb.PlaylistID != "pl-7" {
		t.Errorf("expected playlist pl-7, got %q", hb.PlaylistID)
	}
	if !hb.ScreenOn {
		t.Error("expected screen on")
	}
	if hb.UptimeS <= 0 {
		t.Errorf("expected positive uptime, got %d", hb.UptimeS)
	}
	if hb.Timestamp == "" {
		t.Error("expected non-empty timestamp")
	}
}

func TestSysInfoHelpers(t *testing.T) {
	// These are best-effort on Linux, may return 0 in containers.
	// Just verify they don't panic.
	_ = cpuTemp()
	_ = memUsedPct()
	_ = diskUsedPct("/")
	_ = storageUsedBytes("/")
}
