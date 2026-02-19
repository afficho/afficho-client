package display

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/afficho/afficho-client/internal/config"
)

// ScreenController manages scheduled screen power on/off.
type ScreenController struct {
	cfg       *config.Config
	offHour   int
	offMinute int
	onHour    int
	onMinute  int
	screenOff bool // tracks current state to avoid redundant commands
}

// NewScreenController parses config and returns a controller, or nil if
// screen scheduling is not configured.
func NewScreenController(cfg *config.Config) *ScreenController {
	offTime := strings.TrimSpace(cfg.Display.ScreenOffTime)
	onTime := strings.TrimSpace(cfg.Display.ScreenOnTime)

	if offTime == "" && onTime == "" {
		return nil
	}

	sc := &ScreenController{cfg: cfg}

	if offTime != "" {
		h, m, err := parseHHMM(offTime)
		if err != nil {
			slog.Error("invalid screen_off_time, ignoring", "value", offTime, "error", err)
			return nil
		}
		sc.offHour, sc.offMinute = h, m
	}

	if onTime != "" {
		h, m, err := parseHHMM(onTime)
		if err != nil {
			slog.Error("invalid screen_on_time, ignoring", "value", onTime, "error", err)
			return nil
		}
		sc.onHour, sc.onMinute = h, m
	}

	return sc
}

// Run checks the screen schedule every 60 seconds and powers the display
// on or off accordingly. Blocks until ctx is cancelled.
func (sc *ScreenController) Run(ctx context.Context) {
	slog.Info("screen power schedule active",
		"off", sc.cfg.Display.ScreenOffTime,
		"on", sc.cfg.Display.ScreenOnTime)

	// Check immediately on start.
	sc.evaluate(time.Now())

	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case t := <-ticker.C:
			sc.evaluate(t)
		}
	}
}

func (sc *ScreenController) evaluate(now time.Time) {
	nowMins := now.Hour()*60 + now.Minute()
	offMins := sc.offHour*60 + sc.offMinute
	onMins := sc.onHour*60 + sc.onMinute

	shouldBeOff := false

	hasOff := sc.cfg.Display.ScreenOffTime != ""
	hasOn := sc.cfg.Display.ScreenOnTime != ""

	switch {
	case hasOff && hasOn:
		if offMins < onMins {
			// Off period does not cross midnight: e.g. 22:00–07:00
			shouldBeOff = nowMins >= offMins || nowMins < onMins
		} else {
			// Off period crosses midnight: e.g. 02:00–06:00
			shouldBeOff = nowMins >= offMins && nowMins < onMins
		}
	case hasOff:
		shouldBeOff = nowMins >= offMins
	case hasOn:
		shouldBeOff = nowMins < onMins
	}

	if shouldBeOff && !sc.screenOff {
		slog.Info("screen power: turning off")
		sc.setScreenPower(false)
		sc.screenOff = true
	} else if !shouldBeOff && sc.screenOff {
		slog.Info("screen power: turning on")
		sc.setScreenPower(true)
		sc.screenOff = false
	}
}

// setScreenPower turns the display on or off. Tries vcgencmd (Raspberry Pi)
// first, then falls back to xset dpms.
func (sc *ScreenController) setScreenPower(on bool) {
	// Try vcgencmd first (Raspberry Pi).
	val := "0"
	if on {
		val = "1"
	}
	if _, err := exec.LookPath("vcgencmd"); err == nil {
		cmd := exec.Command("vcgencmd", "display_power", val) //nolint:gosec // trusted value
		if err := cmd.Run(); err == nil {
			return
		}
		slog.Debug("vcgencmd failed, trying xset", "error", err)
	}

	// Fallback: xset dpms.
	action := "off"
	if on {
		action = "on"
	}
	cmd := exec.Command("xset", "dpms", "force", action) //nolint:gosec // trusted value
	if env := sc.cfg.Display.DisplayEnv; env != "" {
		cmd.Env = append(cmd.Environ(), "DISPLAY="+env)
	}
	if err := cmd.Run(); err != nil {
		slog.Warn("failed to set screen power", "on", on, "error", err)
	}
}

// parseHHMM parses a "HH:MM" string and returns hour and minute.
func parseHHMM(s string) (hour, minute int, err error) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("expected HH:MM format, got %q", s)
	}
	h, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil || h < 0 || h > 23 {
		return 0, 0, fmt.Errorf("invalid hour in %q", s)
	}
	m, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil || m < 0 || m > 59 {
		return 0, 0, fmt.Errorf("invalid minute in %q", s)
	}
	return h, m, nil
}
