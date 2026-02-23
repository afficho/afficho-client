// Package cloud implements the client-side connection to Afficho Cloud.
// When enabled, a Connector maintains a persistent WebSocket to the cloud
// endpoint, handles reconnection with exponential backoff, sends device
// registration and heartbeats, and dispatches incoming messages to handlers.
package cloud

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"runtime"
	"sync"
	"time"

	types "github.com/afficho/afficho-types"
	"github.com/coder/websocket"

	"github.com/afficho/afficho-client/internal/config"
)

// MessageHandler is called when a message of a registered type arrives.
type MessageHandler func(payload json.RawMessage)

// Connector maintains a persistent WebSocket connection to the Afficho Cloud.
type Connector struct {
	cfg        config.CloudConfig
	deviceID   string
	appVersion string
	dataDir    string
	startedAt  time.Time
	status     StatusProvider

	mu       sync.Mutex
	conn     *websocket.Conn
	handlers map[string]MessageHandler
}

// New creates a Connector. Call Run to start the connection loop.
func New(cfg config.CloudConfig, deviceID, appVersion, dataDir string) *Connector {
	c := &Connector{
		cfg:        cfg,
		deviceID:   deviceID,
		appVersion: appVersion,
		dataDir:    dataDir,
		startedAt:  time.Now(),
		handlers:   make(map[string]MessageHandler),
	}
	c.handlers[types.TypeHeartbeatAck] = handleHeartbeatAck
	return c
}

// SetStatusProvider sets the provider used to populate heartbeat fields
// (current item, playlist, screen state). Must be called before Run.
func (c *Connector) SetStatusProvider(sp StatusProvider) {
	c.status = sp
}

// Handle registers a handler for a given message type. Must be called before Run.
func (c *Connector) Handle(msgType string, h MessageHandler) {
	c.handlers[msgType] = h
}

// handleHeartbeatAck processes a heartbeat_ack message. Currently logged
// at debug level; future versions may extract pending commands from the payload.
func handleHeartbeatAck(payload json.RawMessage) {
	slog.Debug("cloud: heartbeat acknowledged")
}

// Run connects to the cloud and reconnects on failure until ctx is cancelled.
// It blocks until ctx is done.
func (c *Connector) Run(ctx context.Context) {
	delay := time.Second
	maxDelay := time.Duration(c.cfg.ReconnectMaxDelay) * time.Second
	if maxDelay <= 0 {
		maxDelay = 5 * time.Minute
	}

	for {
		err := c.connect(ctx)
		if ctx.Err() != nil {
			return // shutting down
		}

		slog.Warn("cloud: connection lost, reconnecting", "error", err, "delay", delay)

		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}

		// Exponential backoff: 1s → 2s → 4s → ... → max.
		delay *= 2
		if delay > maxDelay {
			delay = maxDelay
		}
	}
}

// SendMessage marshals and sends a WSMessage over the active connection.
// Returns an error if there is no active connection.
func (c *Connector) SendMessage(ctx context.Context, msg types.WSMessage) error {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()

	if conn == nil {
		return fmt.Errorf("cloud: not connected")
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("cloud: marshal message: %w", err)
	}

	writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return conn.Write(writeCtx, websocket.MessageText, data)
}

// connect dials the cloud endpoint, sends registration, and enters the
// read loop. It returns when the connection drops or ctx is cancelled.
func (c *Connector) connect(ctx context.Context) error {
	slog.Info("cloud: connecting", "endpoint", c.cfg.Endpoint)

	header := http.Header{}
	header.Set("Authorization", "Bearer "+c.cfg.DeviceKey)

	conn, _, err := websocket.Dial(ctx, c.cfg.Endpoint, &websocket.DialOptions{
		HTTPHeader: header,
	})
	if err != nil {
		return fmt.Errorf("cloud: dial: %w", err)
	}

	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		c.conn = nil
		c.mu.Unlock()
		conn.Close(websocket.StatusNormalClosure, "")
	}()

	// Send device registration.
	if err := c.sendRegistration(ctx); err != nil {
		return err
	}

	slog.Info("cloud: connected and registered", "device_id", c.deviceID)

	// Start heartbeat in background — cancelled when connCtx ends.
	connCtx, connCancel := context.WithCancel(ctx)
	defer connCancel()
	go c.startHeartbeat(connCtx)

	return c.readLoop(ctx, conn)
}

// sendRegistration sends a TypeRegister message with device metadata.
func (c *Connector) sendRegistration(ctx context.Context) error {
	reg := types.DeviceRegistration{
		DeviceID:   c.deviceID,
		Hostname:   hostname(),
		Arch:       runtime.GOARCH,
		OSVersion:  osVersion(),
		AppVersion: c.appVersion,
		LocalIP:    localIP(),
	}

	payload, err := json.Marshal(reg)
	if err != nil {
		return fmt.Errorf("cloud: marshal registration: %w", err)
	}

	return c.SendMessage(ctx, types.WSMessage{
		Type:    types.TypeRegister,
		Payload: payload,
	})
}

// readLoop reads messages from the WebSocket and dispatches them to handlers.
func (c *Connector) readLoop(ctx context.Context, conn *websocket.Conn) error {
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return fmt.Errorf("cloud: read: %w", err)
		}

		var msg types.WSMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			slog.Warn("cloud: invalid message", "error", err)
			continue
		}

		slog.Debug("cloud: received message", "type", msg.Type)

		handler, ok := c.handlers[msg.Type]
		if !ok {
			slog.Debug("cloud: no handler for message type", "type", msg.Type)
			continue
		}

		handler(msg.Payload)
	}
}

// hostname returns the system hostname or "unknown".
func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return h
}

// osVersion returns a basic OS description.
func osVersion() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}

// localIP returns the first non-loopback IPv4 address, or "unknown".
func localIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "unknown"
	}
	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() && ipNet.IP.To4() != nil {
			return ipNet.IP.String()
		}
	}
	return "unknown"
}
