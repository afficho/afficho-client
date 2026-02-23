package api

import (
	"encoding/json"
	"testing"
	"time"

	types "github.com/afficho/afficho-types"
)

func TestHubNewEmpty(t *testing.T) {
	h := newHub()
	if got := h.count(); got != 0 {
		t.Errorf("expected 0 clients, got %d", got)
	}
}

func TestHubRegisterUnregister(t *testing.T) {
	h := newHub()

	c1 := h.register()
	if h.count() != 1 {
		t.Errorf("expected 1 client after register, got %d", h.count())
	}

	c2 := h.register()
	if h.count() != 2 {
		t.Errorf("expected 2 clients, got %d", h.count())
	}

	h.unregister(c1)
	if h.count() != 1 {
		t.Errorf("expected 1 client after unregister, got %d", h.count())
	}

	h.unregister(c2)
	if h.count() != 0 {
		t.Errorf("expected 0 clients after unregister all, got %d", h.count())
	}
}

func TestHubBroadcast(t *testing.T) {
	h := newHub()
	c1 := h.register()
	c2 := h.register()
	defer h.unregister(c1)
	defer h.unregister(c2)

	payload, _ := json.Marshal(map[string]string{"id": "test"})
	msg := types.WSMessage{Type: types.TypeCurrent, Payload: payload}
	h.Broadcast(msg)

	// Both clients should receive the message.
	select {
	case got := <-c1.msgs:
		if got.Type != types.TypeCurrent {
			t.Errorf("c1: expected type %q, got %q", types.TypeCurrent, got.Type)
		}
	case <-time.After(time.Second):
		t.Error("c1: timed out waiting for message")
	}

	select {
	case got := <-c2.msgs:
		if got.Type != types.TypeCurrent {
			t.Errorf("c2: expected type %q, got %q", types.TypeCurrent, got.Type)
		}
	case <-time.After(time.Second):
		t.Error("c2: timed out waiting for message")
	}
}

func TestHubBroadcastDropsForSlowClient(t *testing.T) {
	h := newHub()
	c := h.register()
	defer h.unregister(c)

	// Fill the client's buffer (capacity 16).
	for i := range 16 {
		payload, _ := json.Marshal(i)
		h.Broadcast(types.WSMessage{Type: types.TypeCurrent, Payload: payload})
	}

	// Next broadcast should be dropped (not block).
	payload, _ := json.Marshal("overflow")
	h.Broadcast(types.WSMessage{Type: types.TypeCurrent, Payload: payload})

	// Drain and count — should be exactly 16 (the overflow was dropped).
	count := 0
	for range 16 {
		select {
		case <-c.msgs:
			count++
		default:
			t.Fatal("expected message in buffer")
		}
	}

	// No more messages.
	select {
	case msg := <-c.msgs:
		t.Errorf("unexpected extra message: %+v", msg)
	default:
		// Good — buffer is empty.
	}

	if count != 16 {
		t.Errorf("expected 16 messages, got %d", count)
	}
}

func TestHubUnregisterClosesChannel(t *testing.T) {
	h := newHub()
	c := h.register()
	h.unregister(c)

	// Reading from a closed channel should return zero value immediately.
	_, ok := <-c.msgs
	if ok {
		t.Error("expected channel to be closed after unregister")
	}
}
