package display

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"time"

	"github.com/afficho/afficho-client/internal/config"
)

// Browser manages the kiosk browser process.
type Browser struct {
	cfg *config.Config
}

// New creates a Browser controller from the given config.
func New(cfg *config.Config) *Browser {
	return &Browser{cfg: cfg}
}

// Launch starts the browser in kiosk mode and restarts it if it exits unexpectedly.
// Blocks until ctx is cancelled.
func (b *Browser) Launch(ctx context.Context) {
	displayURL := fmt.Sprintf("http://localhost:%d/display", b.cfg.Server.Port)

	args := []string{
		"--kiosk",
		"--noerrdialogs",
		"--disable-infobars",
		"--disable-session-crashed-bubble",
		"--disable-restore-session-state",
		"--autoplay-policy=no-user-gesture-required",
		"--disable-pinch",
		"--overscroll-history-navigation=0",
		"--no-first-run",
		"--check-for-update-interval=604800", // suppress update checks
		displayURL,
	}

	for {
		slog.Info("launching browser", "executable", b.cfg.Display.Browser, "url", displayURL)

		cmd := exec.CommandContext(ctx, b.cfg.Display.Browser, args...)

		if env := b.cfg.Display.DisplayEnv; env != "" {
			// Inherit the process environment and set/override DISPLAY.
			cmd.Env = append(cmd.Environ(), "DISPLAY="+env)
		}

		if err := cmd.Run(); err != nil {
			if ctx.Err() != nil {
				return // normal shutdown
			}
			slog.Error("browser exited unexpectedly, restarting in 3s", "error", err)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
			// brief delay before restart to avoid spin-looping on hard failure
		}
	}
}
