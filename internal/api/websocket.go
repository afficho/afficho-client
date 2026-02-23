package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	types "github.com/afficho/afficho-types"
	"github.com/coder/websocket"
)

const wsWriteTimeout = 5 * time.Second

// handleDisplayWS upgrades to a WebSocket connection and streams display
// messages (current item, reload, alert) to the client.
func (s *Server) handleDisplayWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// Chromium on localhost — no origin check needed.
		InsecureSkipVerify: true,
	})
	if err != nil {
		slog.Error("ws: accept failed", "error", err)
		return
	}
	defer conn.Close(websocket.StatusInternalError, "unexpected close")

	c := s.hub.register()
	defer s.hub.unregister(c)

	// Send the current item immediately so a freshly opened page
	// doesn't have to wait for the next scheduler event.
	msg := buildCurrentMessage(s)
	if err := writeMsg(r.Context(), conn, msg); err != nil {
		return
	}

	// CloseRead returns a context that is cancelled when the client
	// sends a close frame or the connection breaks.
	ctx := conn.CloseRead(r.Context())

	for {
		select {
		case <-ctx.Done():
			conn.Close(websocket.StatusNormalClosure, "")
			return
		case msg, ok := <-c.msgs:
			if !ok {
				return
			}
			if err := writeMsg(ctx, conn, msg); err != nil {
				return
			}
		}
	}
}

// BroadcastCurrent reads the scheduler's current item and broadcasts a
// "current" message to all connected display clients.
func (s *Server) BroadcastCurrent() {
	s.hub.Broadcast(buildCurrentMessage(s))
}

// buildCurrentMessage creates a WSMessage with the current scheduler item.
func buildCurrentMessage(s *Server) types.WSMessage {
	item, ok := s.scheduler.Current()
	if !ok {
		return types.WSMessage{Type: types.TypeCurrent}
	}
	payload, err := json.Marshal(item)
	if err != nil {
		slog.Error("ws: failed to marshal current item", "error", err)
		return types.WSMessage{Type: types.TypeCurrent}
	}
	return types.WSMessage{Type: types.TypeCurrent, Payload: payload}
}

func writeMsg(ctx context.Context, conn *websocket.Conn, msg types.WSMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, wsWriteTimeout)
	defer cancel()
	return conn.Write(ctx, websocket.MessageText, data)
}
