package cloud

import (
	"encoding/json"
	"log/slog"

	types "github.com/afficho/afficho-types"
)

// AlertHandler relays alert and clear_alert messages from the cloud
// to the local display WebSocket.
type AlertHandler struct {
	broadcaster Broadcaster
}

// NewAlertHandler creates an AlertHandler and registers it on the connector
// for TypeAlert and TypeClearAlert messages.
func NewAlertHandler(conn *Connector, broadcaster Broadcaster) *AlertHandler {
	ah := &AlertHandler{broadcaster: broadcaster}
	conn.Handle(types.TypeAlert, ah.handleAlert)
	conn.Handle(types.TypeClearAlert, ah.handleClearAlert)
	return ah
}

// handleAlert forwards the alert payload to connected display clients.
func (ah *AlertHandler) handleAlert(payload json.RawMessage) {
	if ah.broadcaster == nil {
		slog.Warn("cloud: no broadcaster set, cannot relay alert")
		return
	}

	// Validate the payload parses correctly before relaying.
	var alert types.AlertMessage
	if err := json.Unmarshal(payload, &alert); err != nil {
		slog.Error("cloud: invalid alert payload", "error", err)
		return
	}

	slog.Info("cloud: relaying alert to display", "text", alert.Text, "urgency", alert.Urgency)
	ah.broadcaster.Broadcast(types.WSMessage{Type: types.TypeAlert, Payload: payload})
}

// handleClearAlert forwards the clear_alert message to connected display clients.
func (ah *AlertHandler) handleClearAlert(payload json.RawMessage) {
	if ah.broadcaster == nil {
		slog.Warn("cloud: no broadcaster set, cannot relay clear_alert")
		return
	}

	slog.Info("cloud: relaying clear_alert to display")
	ah.broadcaster.Broadcast(types.WSMessage{Type: types.TypeClearAlert, Payload: payload})
}
