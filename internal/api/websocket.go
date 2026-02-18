package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

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
	item, ok := s.scheduler.Current()
	var payload any
	if ok {
		payload = item
	}
	if err := writeMsg(r.Context(), conn, Message{Type: "current", Payload: payload}); err != nil {
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
	item, ok := s.scheduler.Current()
	var payload any
	if ok {
		payload = item
	}
	s.hub.Broadcast(Message{Type: "current", Payload: payload})
}

func writeMsg(ctx context.Context, conn *websocket.Conn, msg Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, wsWriteTimeout)
	defer cancel()
	return conn.Write(ctx, websocket.MessageText, data)
}
