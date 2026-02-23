package cloud

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"time"

	types "github.com/afficho/afficho-types"
)

// Broadcaster sends messages to connected display WebSocket clients.
type Broadcaster interface {
	Broadcast(msg types.WSMessage)
}

// UpdateTrigger triggers an immediate update check.
type UpdateTrigger interface {
	CheckNow()
}

// CommandHandler handles command messages from the cloud.
type CommandHandler struct {
	conn        *Connector
	broadcaster Broadcaster
	updater     UpdateTrigger
	deviceID    string
}

// NewCommandHandler creates a CommandHandler and registers it as a handler
// on the connector for TypeCommand messages.
func NewCommandHandler(conn *Connector, broadcaster Broadcaster, updater UpdateTrigger, deviceID string) *CommandHandler {
	ch := &CommandHandler{
		conn:        conn,
		broadcaster: broadcaster,
		updater:     updater,
		deviceID:    deviceID,
	}
	conn.Handle(types.TypeCommand, ch.handle)
	return ch
}

// handle is the MessageHandler for TypeCommand messages.
func (ch *CommandHandler) handle(payload json.RawMessage) {
	var cmd types.DeviceCommand
	if err := json.Unmarshal(payload, &cmd); err != nil {
		slog.Error("cloud: invalid command payload", "error", err)
		return
	}

	slog.Info("cloud: command received", "command", cmd.Command)

	switch cmd.Command {
	case "reload":
		ch.handleReload()
	case "reboot":
		ch.handleReboot()
	case "update":
		ch.handleUpdate()
	case "screenshot":
		ch.handleScreenshot()
	default:
		slog.Warn("cloud: unknown command", "command", cmd.Command)
	}
}

// handleReload broadcasts a reload message to all connected display clients.
func (ch *CommandHandler) handleReload() {
	if ch.broadcaster == nil {
		slog.Warn("cloud: no broadcaster set, cannot reload display")
		return
	}
	ch.broadcaster.Broadcast(types.WSMessage{Type: types.TypeReload})
	slog.Info("cloud: reload broadcast sent")
}

// handleReboot initiates a system reboot.
func (ch *CommandHandler) handleReboot() {
	slog.Warn("cloud: reboot command received, rebooting in 3 seconds")

	go func() {
		time.Sleep(3 * time.Second)

		// Try systemctl first, fall back to reboot command.
		if err := exec.Command("systemctl", "reboot").Run(); err != nil {
			slog.Warn("cloud: systemctl reboot failed, trying reboot", "error", err)
			if err := exec.Command("reboot").Run(); err != nil {
				slog.Error("cloud: reboot failed", "error", err)
			}
		}
	}()
}

// handleUpdate triggers an immediate update check.
func (ch *CommandHandler) handleUpdate() {
	if ch.updater == nil {
		slog.Warn("cloud: updater not available, cannot trigger update")
		return
	}
	slog.Info("cloud: triggering update check")
	go ch.updater.CheckNow()
}

// handleScreenshot captures the screen and sends the result back to the cloud.
func (ch *CommandHandler) handleScreenshot() {
	go func() {
		imgData, err := captureScreen()
		if err != nil {
			slog.Error("cloud: screenshot capture failed", "error", err)
			return
		}

		resp := types.ScreenshotResponse{
			DeviceID:    ch.deviceID,
			ImageBase64: base64.StdEncoding.EncodeToString(imgData),
			Timestamp:   time.Now().UTC().Format(time.RFC3339),
		}

		payload, err := json.Marshal(resp)
		if err != nil {
			slog.Error("cloud: marshal screenshot response", "error", err)
			return
		}

		if err := ch.conn.SendMessage(context.Background(), types.WSMessage{
			Type:    types.TypeScreenshot,
			Payload: payload,
		}); err != nil {
			slog.Error("cloud: send screenshot failed", "error", err)
		} else {
			slog.Info("cloud: screenshot sent")
		}
	}()
}

// captureScreen takes a screenshot using available system tools.
// It tries scrot first (common on Raspberry Pi), then falls back to
// import (ImageMagick).
func captureScreen() ([]byte, error) {
	tmpFile := fmt.Sprintf("/tmp/afficho-screenshot-%d.png", time.Now().UnixNano())
	defer os.Remove(tmpFile)

	// Try scrot first.
	if err := exec.Command("scrot", tmpFile).Run(); err == nil {
		return os.ReadFile(tmpFile)
	}

	// Fall back to import (ImageMagick).
	if err := exec.Command("import", "-window", "root", tmpFile).Run(); err == nil {
		return os.ReadFile(tmpFile)
	}

	// Fall back to xwd + convert.
	if err := exec.Command("sh", "-c",
		fmt.Sprintf("xwd -root -silent | convert xwd:- png:%s", tmpFile),
	).Run(); err == nil {
		return os.ReadFile(tmpFile)
	}

	return nil, fmt.Errorf("no screenshot tool available (tried scrot, import, xwd)")
}
