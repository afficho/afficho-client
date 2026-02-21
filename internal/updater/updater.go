// Package updater checks GitHub releases for new versions and applies updates.
//
// The updater is opt-in (disabled by default). When enabled it periodically
// polls the GitHub Releases API, downloads the correct architecture binary,
// verifies its SHA256 checksum, and stages it for the next service restart.
package updater

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/afficho/afficho-client/internal/config"
)

// Updater checks for new releases and stages updates.
type Updater struct {
	currentVersion string
	goarm          string
	cfg            *config.Config
	interval       time.Duration

	mu            sync.RWMutex
	latestVersion string
	updateAvail   bool
	lastCheck     time.Time
	lastErr       error
}

// Status holds the current state of the updater for API consumers.
type Status struct {
	Enabled         bool      `json:"enabled"`
	CurrentVersion  string    `json:"current_version"`
	LatestVersion   string    `json:"latest_version"`
	UpdateAvailable bool      `json:"update_available"`
	LastCheck       time.Time `json:"last_check"`
	NextCheck       time.Time `json:"next_check"`
	LastError       string    `json:"last_error,omitempty"`
}

// New creates an Updater. Returns nil if auto-update is disabled.
func New(version, goarm string, cfg *config.Config) (*Updater, error) {
	if !cfg.Update.Enabled {
		return nil, nil
	}

	interval, err := time.ParseDuration(cfg.Update.CheckInterval)
	if err != nil {
		return nil, fmt.Errorf("parsing update check_interval %q: %w", cfg.Update.CheckInterval, err)
	}
	if interval < 5*time.Minute {
		interval = 5 * time.Minute
	}

	return &Updater{
		currentVersion: version,
		goarm:          goarm,
		cfg:            cfg,
		interval:       interval,
	}, nil
}

// Run starts the periodic update check loop. It blocks until ctx is cancelled.
func (u *Updater) Run(ctx context.Context) {
	slog.Info("auto-updater started",
		"interval", u.interval,
		"channel", u.cfg.Update.Channel,
	)

	// Check once shortly after startup (30s delay to let the service settle).
	select {
	case <-time.After(30 * time.Second):
		u.check()
	case <-ctx.Done():
		return
	}

	ticker := time.NewTicker(u.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			u.check()
		case <-ctx.Done():
			slog.Info("auto-updater stopped")
			return
		}
	}
}

// CheckNow triggers an immediate update check and returns the status.
func (u *Updater) CheckNow() Status {
	u.check()
	return u.Status()
}

// Status returns the current updater state.
func (u *Updater) Status() Status {
	u.mu.RLock()
	defer u.mu.RUnlock()

	var errStr string
	if u.lastErr != nil {
		errStr = u.lastErr.Error()
	}

	return Status{
		Enabled:         true,
		CurrentVersion:  u.currentVersion,
		LatestVersion:   u.latestVersion,
		UpdateAvailable: u.updateAvail,
		LastCheck:       u.lastCheck,
		NextCheck:       u.lastCheck.Add(u.interval),
		LastError:       errStr,
	}
}

// check performs a single update check cycle.
func (u *Updater) check() {
	slog.Debug("checking for updates", "channel", u.cfg.Update.Channel)

	release, err := fetchLatestRelease(u.cfg.Update.Channel)

	u.mu.Lock()
	u.lastCheck = time.Now()
	u.lastErr = err
	u.mu.Unlock()

	if err != nil {
		slog.Warn("update check failed", "error", err)
		return
	}
	if release == nil {
		slog.Debug("no release found")
		return
	}

	latest := normaliseVersion(release.TagName)
	current := normaliseVersion(u.currentVersion)

	u.mu.Lock()
	u.latestVersion = latest
	u.mu.Unlock()

	if !isNewer(current, latest) {
		slog.Debug("already up to date", "current", current, "latest", latest)
		u.mu.Lock()
		u.updateAvail = false
		u.mu.Unlock()
		return
	}

	slog.Info("new version available", "current", current, "latest", latest)

	u.mu.Lock()
	u.updateAvail = true
	u.mu.Unlock()

	if err := u.applyUpdate(release); err != nil {
		slog.Error("failed to apply update", "version", latest, "error", err)
		u.mu.Lock()
		u.lastErr = err
		u.mu.Unlock()
		return
	}

	slog.Info("update staged successfully, restart to apply", "version", latest)
}
