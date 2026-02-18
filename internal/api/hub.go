package api

import (
	"log/slog"
	"sync"
)

// Message is the WebSocket message envelope sent to display clients.
// The same format is used for local WebSocket and (future) cloud-pushed messages.
type Message struct {
	Type    string `json:"type"`
	Payload any    `json:"payload,omitempty"`
}

// wsClient is a single connected display WebSocket.
type wsClient struct {
	msgs chan Message
}

// Hub tracks all connected display WebSocket clients and broadcasts
// messages to them. Safe for concurrent use.
type Hub struct {
	mu      sync.Mutex
	clients map[*wsClient]struct{}
}

func newHub() *Hub {
	return &Hub{
		clients: make(map[*wsClient]struct{}),
	}
}

// register adds a new client and returns it. The caller must call
// unregister when the connection closes.
func (h *Hub) register() *wsClient {
	c := &wsClient{
		msgs: make(chan Message, 16),
	}
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
	slog.Debug("ws: client registered", "clients", h.count())
	return c
}

// unregister removes a client and closes its channel.
func (h *Hub) unregister(c *wsClient) {
	h.mu.Lock()
	delete(h.clients, c)
	close(c.msgs)
	h.mu.Unlock()
	slog.Debug("ws: client unregistered", "clients", h.count())
}

// Broadcast sends a message to every connected client.
// Slow clients that can't keep up have their message dropped.
func (h *Hub) Broadcast(msg Message) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for c := range h.clients {
		select {
		case c.msgs <- msg:
		default:
			slog.Debug("ws: dropping message for slow client")
		}
	}
}

func (h *Hub) count() int {
	// Caller may or may not hold mu — use len directly since map len is safe.
	return len(h.clients)
}
