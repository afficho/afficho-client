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
		_, _, _ = conn.Read(r.Context())

		// Send a command message to the client.
		cmd := types.DeviceCommand{Command: "reload"}
		payload, _ := json.Marshal(cmd)
		msg := types.WSMessage{Type: types.TypeCommand, Payload: payload}
		data, _ := json.Marshal(msg)

		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		_ = conn.Write(ctx, websocket.MessageText, data)

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
		_, _, _ = conn.Read(r.Context())
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

func TestConnectorDeviceID(t *testing.T) {
	c := New(config.CloudConfig{}, "my-device-id", "dev", t.TempDir())
	if got := c.DeviceID(); got != "my-device-id" {
		t.Errorf("expected DeviceID 'my-device-id', got %q", got)
	}
}

func TestConnectorInitialState(t *testing.T) {
	c := New(config.CloudConfig{}, "dev-1", "dev", t.TempDir())

	if c.Connected() {
		t.Error("new connector should not be connected")
	}
	if !c.LastConnectedAt().IsZero() {
		t.Error("new connector should have zero LastConnectedAt")
	}
}

func TestConnectorConnectedState(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")
		// Read registration, then keep alive.
		_, _, _ = conn.Read(r.Context())
		time.Sleep(500 * time.Millisecond)
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	c := New(config.CloudConfig{
		Enabled:           true,
		Endpoint:          wsURL,
		DeviceKey:         "key",
		ReconnectMaxDelay: 1,
	}, "dev-1", "dev", t.TempDir())

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go c.Run(ctx)

	// Wait until connected.
	deadline := time.After(2 * time.Second)
	for !c.Connected() {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for Connected() to become true")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	if c.LastConnectedAt().IsZero() {
		t.Error("expected non-zero LastConnectedAt after connection")
	}
}

func TestConnectorOnConnectCallbacks(t *testing.T) {
	var callCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")
		_, _, _ = conn.Read(r.Context())
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

	c.OnConnect(func() { callCount.Add(1) })
	c.OnConnect(func() { callCount.Add(1) })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go c.Run(ctx)

	// Wait for callbacks to fire.
	deadline := time.After(2 * time.Second)
	for callCount.Load() < 2 {
		select {
		case <-deadline:
			t.Fatalf("expected at least 2 callback invocations, got %d", callCount.Load())
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func TestConnectorDisconnectedAfterClose(t *testing.T) {
	// Use a channel to signal the server to close the connection
	// after we've observed the connected state.
	closeConn := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		// Read registration, then wait for signal to close.
		_, _, _ = conn.Read(r.Context())
		<-closeConn
		conn.Close(websocket.StatusNormalClosure, "")
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	c := New(config.CloudConfig{
		Enabled:           true,
		Endpoint:          wsURL,
		DeviceKey:         "key",
		ReconnectMaxDelay: 30, // long delay so we can check disconnected state
	}, "dev-1", "dev", t.TempDir())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go c.Run(ctx)

	// Wait until connected.
	deadline := time.After(2 * time.Second)
	for !c.Connected() {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for connection")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	// Now tell the server to close the connection.
	close(closeConn)

	// Wait until disconnected.
	deadline = time.After(2 * time.Second)
	for c.Connected() {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for disconnection")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	// LastConnectedAt should still be set even after disconnect.
	if c.LastConnectedAt().IsZero() {
		t.Error("LastConnectedAt should persist after disconnect")
	}
}
