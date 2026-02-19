package display

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"time"

	"github.com/afficho/afficho-client/internal/config"
)

// browserCandidates is the ordered list of executables tried during auto-detection.
var browserCandidates = []string{
	"chromium-browser",
	"chromium",
	"google-chrome",
	"brave-browser",
}

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
	// Resolve the browser executable.
	browser := b.resolveBrowser()
	if browser == "" {
		slog.Error("no browser found — display will not launch")
		return
	}

	displayURL := fmt.Sprintf("http://localhost:%d/display", b.cfg.Server.Port)

	// Disable screensaver / DPMS before launching (X11 only, non-fatal).
	b.disableScreensaver()

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
	}

	// Wayland support.
	if b.isWayland() {
		args = append(args,
			"--ozone-platform=wayland",
			"--enable-features=UseOzonePlatform",
		)
	}

	args = append(args, displayURL)

	for {
		slog.Info("launching browser", "executable", browser, "url", displayURL)

		cmd := exec.CommandContext(ctx, browser, args...)

		// Set display environment for X11.
		if env := b.cfg.Display.DisplayEnv; env != "" && !b.isWayland() {
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

// resolveBrowser returns the browser executable to use. If the config value is
// "auto" or empty, it tries a list of known browsers via exec.LookPath.
func (b *Browser) resolveBrowser() string {
	configured := b.cfg.Display.Browser
	if configured != "" && configured != "auto" {
		return configured
	}

	for _, name := range browserCandidates {
		if path, err := exec.LookPath(name); err == nil {
			slog.Info("auto-detected browser", "executable", path)
			return path
		}
	}

	slog.Error("auto-detect failed: no supported browser found",
		"tried", browserCandidates)
	return ""
}

// isWayland returns true when the display platform should use Wayland.
func (b *Browser) isWayland() bool {
	switch b.cfg.Display.Platform {
	case "wayland":
		return true
	case "x11":
		return false
	default: // "auto" or empty
		return os.Getenv("WAYLAND_DISPLAY") != ""
	}
}

// disableScreensaver runs xset commands to disable the X11 screensaver and DPMS.
// Failures are logged as warnings (not fatal — may be Wayland or headless).
func (b *Browser) disableScreensaver() {
	if b.isWayland() {
		return // xset is X11 only
	}
	if b.cfg.Display.DisplayEnv == "" {
		return
	}

	commands := [][]string{
		{"xset", "s", "off"},
		{"xset", "-dpms"},
		{"xset", "s", "noblank"},
	}

	for _, args := range commands {
		cmd := exec.Command(args[0], args[1:]...) //nolint:gosec // trusted static args
		cmd.Env = append(os.Environ(), "DISPLAY="+b.cfg.Display.DisplayEnv)
		if err := cmd.Run(); err != nil {
			slog.Warn("failed to disable screensaver", "cmd", args, "error", err)
			return // if xset isn't available, skip the rest
		}
	}

	slog.Info("screensaver and DPMS disabled")
}
