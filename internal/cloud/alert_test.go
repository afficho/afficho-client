package cloud

import (
	"encoding/json"
	"testing"

	types "github.com/afficho/afficho-types"

	"github.com/afficho/afficho-client/internal/config"
)

func TestAlertHandlerRegistered(t *testing.T) {
	conn := New(config.CloudConfig{}, "test-device", "dev", t.TempDir())
	NewAlertHandler(conn, &mockBroadcaster{})

	if _, ok := conn.handlers[types.TypeAlert]; !ok {
		t.Fatal("expected alert handler to be registered")
	}
	if _, ok := conn.handlers[types.TypeClearAlert]; !ok {
		t.Fatal("expected clear_alert handler to be registered")
	}
}

func TestAlertRelaysBroadcast(t *testing.T) {
	conn := New(config.CloudConfig{}, "test-device", "dev", t.TempDir())
	bc := &mockBroadcaster{}
	ah := NewAlertHandler(conn, bc)

	alert := types.AlertMessage{Text: "Fire drill", Urgency: "warning", TTLS: 60}
	payload, _ := json.Marshal(alert)

	ah.handleAlert(payload)

	msgs := bc.messages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 broadcast, got %d", len(msgs))
	}
	if msgs[0].Type != types.TypeAlert {
		t.Errorf("expected type %q, got %q", types.TypeAlert, msgs[0].Type)
	}

	// Verify the payload was forwarded intact.
	var relayed types.AlertMessage
	if err := json.Unmarshal(msgs[0].Payload, &relayed); err != nil {
		t.Fatalf("unmarshal relayed payload: %v", err)
	}
	if relayed.Text != "Fire drill" {
		t.Errorf("expected text 'Fire drill', got %q", relayed.Text)
	}
}

func TestAlertRejectsInvalidPayload(t *testing.T) {
	conn := New(config.CloudConfig{}, "test-device", "dev", t.TempDir())
	bc := &mockBroadcaster{}
	ah := NewAlertHandler(conn, bc)

	ah.handleAlert(json.RawMessage(`{invalid`))

	msgs := bc.messages()
	if len(msgs) != 0 {
		t.Error("expected no broadcast for invalid payload")
	}
}

func TestClearAlertRelaysBroadcast(t *testing.T) {
	conn := New(config.CloudConfig{}, "test-device", "dev", t.TempDir())
	bc := &mockBroadcaster{}
	ah := NewAlertHandler(conn, bc)

	ah.handleClearAlert(nil)

	msgs := bc.messages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 broadcast, got %d", len(msgs))
	}
	if msgs[0].Type != types.TypeClearAlert {
		t.Errorf("expected type %q, got %q", types.TypeClearAlert, msgs[0].Type)
	}
}

func TestAlertNoBroadcaster(t *testing.T) {
	conn := New(config.CloudConfig{}, "test-device", "dev", t.TempDir())
	ah := NewAlertHandler(conn, nil)

	alert := types.AlertMessage{Text: "Test", Urgency: "info"}
	payload, _ := json.Marshal(alert)

	// Should not panic.
	ah.handleAlert(payload)
	ah.handleClearAlert(nil)
}

func TestAlertDispatchViaConnector(t *testing.T) {
	conn := New(config.CloudConfig{}, "test-device", "dev", t.TempDir())
	bc := &mockBroadcaster{}
	_ = NewAlertHandler(conn, bc)

	alert := types.AlertMessage{Text: "Emergency", Urgency: "critical"}
	payload, _ := json.Marshal(alert)

	// Dispatch via the connector handler map (simulating cloud message arrival).
	conn.handlers[types.TypeAlert](payload)

	msgs := bc.messages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 broadcast after dispatch, got %d", len(msgs))
	}
	if msgs[0].Type != types.TypeAlert {
		t.Errorf("expected type %q, got %q", types.TypeAlert, msgs[0].Type)
	}
}
