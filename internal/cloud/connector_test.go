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

func TestHandlerRegistration(t *testing.T) {
	c := New(config.CloudConfig{}, "test-device", "dev", t.TempDir())

	called := false
	c.Handle(types.TypeCommand, func(payload json.RawMessage) {
		called = true
	})

	if _, ok := c.handlers[types.TypeCommand]; !ok {
		t.Fatal("expected handler to be registered")
	}

	c.handlers[types.TypeCommand](nil)
	if !called {
		t.Fatal("expected handler to be called")
	}
}

func TestSendMessageNoConnection(t *testing.T) {
	c := New(config.CloudConfig{}, "test-device", "dev", t.TempDir())

	err := c.SendMessage(context.Background(), types.WSMessage{Type: types.TypeHeartbeat})
	if err == nil {
		t.Fatal("expected error when not connected")
	}
}

func TestConnectorRegistersOnConnect(t *testing.T) {
	var registered atomic.Bool
	var regMsg types.WSMessage

	// Stand up a test WebSocket server.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify auth header.
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-key" {
			t.Errorf("expected Bearer test-key, got %q", auth)
		}

		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")

		// Read the registration message.
		_, data, err := conn.Read(r.Context())
		if err != nil {
			return
		}

		if err := json.Unmarshal(data, &regMsg); err != nil {
			t.Errorf("unmarshal: %v", err)
			return
		}
		registered.Store(true)

		// Keep connection alive briefly so the read loop runs.
		time.Sleep(100 * time.Millisecond)
	}))
	defer srv.Close()

	// Convert http:// to ws://.
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")

	c := New(config.CloudConfig{
		Enabled:           true,
		Endpoint:          wsURL,
		DeviceKey:         "test-key",
		ReconnectMaxDelay: 1,
	}, "device-123", "v1.0.0", t.TempDir())

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Run connector in background — it will connect, register, then the
	// server closes, and it'll try to reconnect until ctx expires.
	go c.Run(ctx)

	// Wait for registration.
	deadline := time.After(2 * time.Second)
	for !registered.Load() {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for registration")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	if regMsg.Type != types.TypeRegister {
		t.Errorf("expected type %q, got %q", types.TypeRegister, regMsg.Type)
	}

	var reg types.DeviceRegistration
	if err := json.Unmarshal(regMsg.Payload, &reg); err != nil {
		t.Fatalf("unmarshal registration payload: %v", err)
	}
	if reg.DeviceID != "device-123" {
		t.Errorf("expected device ID device-123, got %q", reg.DeviceID)
	}
	if reg.AppVersion != "v1.0.0" {
		t.Errorf("expected app version v1.0.0, got %q", reg.AppVersion)
	}
}

func TestConnectorDispatchesMessages(t *testing.T) {
	var received atomic.Bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")

		// Read the registration message (discard).
		conn.Read(r.Context())

		// Send a command message to the client.
		cmd := types.DeviceCommand{Command: "reload"}
		payload, _ := json.Marshal(cmd)
		msg := types.WSMessage{Type: types.TypeCommand, Payload: payload}
		data, _ := json.Marshal(msg)

		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		conn.Write(ctx, websocket.MessageText, data)

		// Keep alive for the client to process.
		time.Sleep(200 * time.Millisecond)
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")

	c := New(config.CloudConfig{
		Enabled:           true,
		Endpoint:          wsURL,
		DeviceKey:         "key",
		ReconnectMaxDelay: 1,
	}, "dev-1", "dev", t.TempDir())

	c.Handle(types.TypeCommand, func(payload json.RawMessage) {
		var cmd types.DeviceCommand
		if err := json.Unmarshal(payload, &cmd); err != nil {
			t.Errorf("unmarshal command: %v", err)
			return
		}
		if cmd.Command != "reload" {
			t.Errorf("expected command reload, got %q", cmd.Command)
		}
		received.Store(true)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go c.Run(ctx)

	deadline := time.After(2 * time.Second)
	for !received.Load() {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for command dispatch")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func TestConnectorReconnects(t *testing.T) {
	var connectCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		connectCount.Add(1)
		// Read registration then immediately close to force reconnect.
		conn.Read(r.Context())
		conn.Close(websocket.StatusNormalClosure, "test close")
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")

	c := New(config.CloudConfig{
		Enabled:           true,
		Endpoint:          wsURL,
		DeviceKey:         "key",
		ReconnectMaxDelay: 1, // 1s max to speed up test
	}, "dev-1", "dev", t.TempDir())

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go c.Run(ctx)

	// Wait for at least 2 connections (initial + reconnect).
	deadline := time.After(3 * time.Second)
	for connectCount.Load() < 2 {
		select {
		case <-deadline:
			t.Fatalf("expected at least 2 connections, got %d", connectCount.Load())
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}
}
