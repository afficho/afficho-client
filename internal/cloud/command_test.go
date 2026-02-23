package cloud

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	types "github.com/afficho/afficho-types"

	"github.com/afficho/afficho-client/internal/config"
)

// mockBroadcaster records messages sent via Broadcast.
type mockBroadcaster struct {
	mu   sync.Mutex
	msgs []types.WSMessage
}

func (m *mockBroadcaster) Broadcast(msg types.WSMessage) {
	m.mu.Lock()
	m.msgs = append(m.msgs, msg)
	m.mu.Unlock()
}

func (m *mockBroadcaster) messages() []types.WSMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]types.WSMessage, len(m.msgs))
	copy(out, m.msgs)
	return out
}

// mockUpdater records whether CheckNow was called.
type mockUpdater struct {
	mu     sync.Mutex
	called bool
}

func (m *mockUpdater) CheckNow() {
	m.mu.Lock()
	m.called = true
	m.mu.Unlock()
}

func (m *mockUpdater) wasCalled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.called
}

func newTestCommandHandler(t *testing.T) (*CommandHandler, *mockBroadcaster, *mockUpdater) {
	t.Helper()
	conn := New(config.CloudConfig{}, "test-device", "dev", t.TempDir())
	bc := &mockBroadcaster{}
	upd := &mockUpdater{}
	ch := NewCommandHandler(conn, bc, upd, "test-device")
	return ch, bc, upd
}

func TestCommandHandlerRegistered(t *testing.T) {
	conn := New(config.CloudConfig{}, "test-device", "dev", t.TempDir())
	NewCommandHandler(conn, nil, nil, "test-device")

	if _, ok := conn.handlers[types.TypeCommand]; !ok {
		t.Fatal("expected command handler to be registered")
	}
}

func TestCommandReload(t *testing.T) {
	ch, bc, _ := newTestCommandHandler(t)

	payload, _ := json.Marshal(types.DeviceCommand{Command: "reload"})
	ch.handle(payload)

	msgs := bc.messages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 broadcast message, got %d", len(msgs))
	}
	if msgs[0].Type != types.TypeReload {
		t.Errorf("expected type %q, got %q", types.TypeReload, msgs[0].Type)
	}
}

func TestCommandReloadNoBroadcaster(t *testing.T) {
	conn := New(config.CloudConfig{}, "test-device", "dev", t.TempDir())
	ch := NewCommandHandler(conn, nil, nil, "test-device")

	payload, _ := json.Marshal(types.DeviceCommand{Command: "reload"})
	// Should not panic.
	ch.handle(payload)
}

func TestCommandUpdate(t *testing.T) {
	ch, _, upd := newTestCommandHandler(t)

	payload, _ := json.Marshal(types.DeviceCommand{Command: "update"})
	ch.handle(payload)

	// CheckNow is dispatched in a goroutine; poll briefly.
	deadline := time.After(time.Second)
	for !upd.wasCalled() {
		select {
		case <-deadline:
			t.Fatal("expected updater.CheckNow to be called")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
}

func TestCommandUpdateNoUpdater(t *testing.T) {
	conn := New(config.CloudConfig{}, "test-device", "dev", t.TempDir())
	ch := NewCommandHandler(conn, &mockBroadcaster{}, nil, "test-device")

	payload, _ := json.Marshal(types.DeviceCommand{Command: "update"})
	// Should not panic.
	ch.handle(payload)
}

func TestCommandUnknown(t *testing.T) {
	ch, bc, _ := newTestCommandHandler(t)

	payload, _ := json.Marshal(types.DeviceCommand{Command: "unknown-cmd"})
	ch.handle(payload)

	// No broadcast should be sent for unknown commands.
	msgs := bc.messages()
	if len(msgs) != 0 {
		t.Errorf("expected no broadcast for unknown command, got %d", len(msgs))
	}
}

func TestCommandDispatchViaHandler(t *testing.T) {
	conn := New(config.CloudConfig{}, "test-device", "dev", t.TempDir())
	bc := &mockBroadcaster{}
	_ = NewCommandHandler(conn, bc, nil, "test-device")

	// Simulate receiving a command message via the connector handler map.
	payload, _ := json.Marshal(types.DeviceCommand{Command: "reload"})
	conn.handlers[types.TypeCommand](payload)

	msgs := bc.messages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 broadcast after dispatch, got %d", len(msgs))
	}
	if msgs[0].Type != types.TypeReload {
		t.Errorf("expected type %q, got %q", types.TypeReload, msgs[0].Type)
	}
}
