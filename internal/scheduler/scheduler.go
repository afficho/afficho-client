package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/afficho/afficho-client/internal/content"
	"github.com/afficho/afficho-client/internal/db"
)

// Item represents a single content item in the active playback queue.
type Item struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"`   // "image" | "video" | "url" | "html"
	Source      string `json:"source"` // local /media/... path or external URL
	DurationS   int    `json:"duration_s"`
	AllowPopups bool   `json:"allow_popups"` // add allow-popups to iframe sandbox
}

// Scheduler manages the active playlist and tracks the current playback position.
type Scheduler struct {
	db      *db.DB
	content *content.Manager

	mu      sync.RWMutex
	queue   []Item
	current int

	// activePlaylistID tracks which playlist is currently loaded so we
	// only reset position when the active playlist actually changes.
	activePlaylistID string

	// advanceTimer fires when the current item's duration expires.
	advanceTimer *time.Timer
	// advancedAt records when the current item started displaying.
	advancedAt time.Time

	// reload is signalled after content or playlist changes to trigger
	// an immediate queue refresh without waiting for the ticker.
	reload chan struct{}

	// OnChange is called after the current item changes (advance or reload).
	// Set this before calling Run. Safe to leave nil.
	OnChange func()
}

// New creates a Scheduler. Call Run to start the background loop.
func New(database *db.DB, mgr *content.Manager) *Scheduler {
	return &Scheduler{
		db:      database,
		content: mgr,
		reload:  make(chan struct{}, 1),
	}
}

// Current returns the currently active content item and whether one exists.
func (s *Scheduler) Current() (Item, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.queue) == 0 {
		return Item{}, false
	}
	return s.queue[s.current%len(s.queue)], true
}

// Advance moves to the next item in the queue and returns it.
func (s *Scheduler) Advance() (Item, bool) {
	s.mu.Lock()
	if len(s.queue) == 0 {
		s.mu.Unlock()
		return Item{}, false
	}
	s.current = (s.current + 1) % len(s.queue)
	s.advancedAt = time.Now()
	item := s.queue[s.current]
	s.mu.Unlock()

	s.resetTimer()
	s.notifyChange()
	return item, true
}

// Queue returns a snapshot of the full playback queue.
func (s *Scheduler) Queue() []Item {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]Item, len(s.queue))
	copy(out, s.queue)
	return out
}

// TriggerReload signals the scheduler to reload the queue immediately.
// Safe to call from any goroutine; non-blocking.
func (s *Scheduler) TriggerReload() {
	select {
	case s.reload <- struct{}{}:
	default:
	}
}

// Run starts the scheduling loop. Blocks until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	if err := s.reloadQueue(); err != nil {
		slog.Error("scheduler: initial queue load failed", "error", err)
	}

	// Reload periodically to pick up external changes.
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		// Build a channel for the advance timer; nil if no timer is set.
		var timerC <-chan time.Time
		if s.advanceTimer != nil {
			timerC = s.advanceTimer.C
		}

		select {
		case <-ctx.Done():
			if s.advanceTimer != nil {
				s.advanceTimer.Stop()
			}
			return
		case <-ticker.C:
			if err := s.reloadQueue(); err != nil {
				slog.Error("scheduler: queue reload failed", "error", err)
			}
		case <-s.reload:
			if err := s.reloadQueue(); err != nil {
				slog.Error("scheduler: queue reload failed", "error", err)
			}
		case <-timerC:
			slog.Debug("scheduler: auto-advancing")
			s.Advance()
		}
	}
}

// resetTimer stops any existing advance timer and starts a new one based on
// the current item's duration. Must be called without holding mu.
func (s *Scheduler) resetTimer() {
	if s.advanceTimer != nil {
		s.advanceTimer.Stop()
	}
	s.mu.RLock()
	if len(s.queue) == 0 {
		s.mu.RUnlock()
		s.advanceTimer = nil
		return
	}
	dur := time.Duration(s.queue[s.current%len(s.queue)].DurationS) * time.Second
	s.mu.RUnlock()
	s.advanceTimer = time.NewTimer(dur)
}

// SecondsUntilNext returns the estimated seconds until the scheduler auto-advances.
// Returns 0 if the queue is empty.
func (s *Scheduler) SecondsUntilNext() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.queue) == 0 {
		return 0
	}
	item := s.queue[s.current%len(s.queue)]
	elapsed := time.Since(s.advancedAt).Seconds()
	remaining := float64(item.DurationS) - elapsed
	if remaining < 0 {
		return 0
	}
	return remaining
}

// reloadQueue evaluates schedules, loads the appropriate playlist, and rebuilds the queue.
func (s *Scheduler) reloadQueue() error {
	playlistID := s.evaluateSchedules(time.Now())

	var items []Item
	var err error
	if playlistID != "" {
		items, err = s.loadPlaylist(playlistID)
	} else {
		items, err = s.loadDefaultPlaylist()
	}
	if err != nil {
		return err
	}

	s.mu.Lock()
	playlistChanged := s.activePlaylistID != playlistID
	s.activePlaylistID = playlistID

	// Reset position and timer when the playlist changed, position is out
	// of bounds, or the queue was empty and now has items (need to start
	// the advance timer). Otherwise keep the running timer so items with
	// duration >= 30s aren't perpetually restarted by the periodic reload.
	needsReset := playlistChanged || s.current >= len(items) || (len(s.queue) == 0 && len(items) > 0)
	if needsReset {
		s.current = 0
		s.advancedAt = time.Now()
	}
	s.queue = items
	s.mu.Unlock()

	if needsReset {
		s.resetTimer()
	}
	slog.Debug("scheduler: queue reloaded", "items", len(items), "playlist_id", playlistID, "changed", playlistChanged)
	s.notifyChange()
	return nil
}

// ActivePlaylistID returns the ID of the schedule-selected playlist, or "" for default.
func (s *Scheduler) ActivePlaylistID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activePlaylistID
}

// notifyChange calls the OnChange callback if set.
// Must be called without holding mu.
func (s *Scheduler) notifyChange() {
	if s.OnChange != nil {
		s.OnChange()
	}
}

func (s *Scheduler) loadDefaultPlaylist() ([]Item, error) {
	rows, err := s.db.Query(`
		SELECT
			ci.id,
			ci.name,
			ci.type,
			ci.source,
			COALESCE(pi.duration_override_s, ci.duration_s) AS duration_s,
			ci.allow_popups
		FROM playlists p
		JOIN playlist_items pi ON pi.playlist_id = p.id
		JOIN content_items  ci ON ci.id = pi.content_id
		WHERE p.is_default = 1
		ORDER BY pi.position ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []Item
	for rows.Next() {
		var it Item
		var popups int
		if err := rows.Scan(&it.ID, &it.Name, &it.Type, &it.Source, &it.DurationS, &popups); err != nil {
			return nil, err
		}
		it.AllowPopups = popups != 0
		items = append(items, it)
	}
	return items, rows.Err()
}

// loadPlaylist loads items from a specific playlist by ID.
func (s *Scheduler) loadPlaylist(playlistID string) ([]Item, error) {
	rows, err := s.db.Query(`
		SELECT
			ci.id,
			ci.name,
			ci.type,
			ci.source,
			COALESCE(pi.duration_override_s, ci.duration_s) AS duration_s,
			ci.allow_popups
		FROM playlists p
		JOIN playlist_items pi ON pi.playlist_id = p.id
		JOIN content_items  ci ON ci.id = pi.content_id
		WHERE p.id = ?
		ORDER BY pi.position ASC
	`, playlistID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []Item
	for rows.Next() {
		var it Item
		var popups int
		if err := rows.Scan(&it.ID, &it.Name, &it.Type, &it.Source, &it.DurationS, &popups); err != nil {
			return nil, err
		}
		it.AllowPopups = popups != 0
		items = append(items, it)
	}
	return items, rows.Err()
}

// evaluateSchedules checks all schedules and returns the playlist_id of the
// highest-priority schedule whose time window is currently active.
// Returns "" if no schedule is active (use the default playlist).
func (s *Scheduler) evaluateSchedules(now time.Time) string {
	rows, err := s.db.Query(`
		SELECT playlist_id, cron_expr, priority
		FROM schedules
		ORDER BY priority DESC
	`)
	if err != nil {
		slog.Error("scheduler: querying schedules", "error", err)
		return ""
	}
	defer rows.Close()

	for rows.Next() {
		var playlistID, cronExpr string
		var priority int
		if err := rows.Scan(&playlistID, &cronExpr, &priority); err != nil {
			slog.Error("scheduler: scanning schedule row", "error", err)
			continue
		}
		tw, err := ParseTimeWindow(cronExpr)
		if err != nil {
			slog.Warn("scheduler: invalid cron_expr, skipping", "expr", cronExpr, "error", err)
			continue
		}
		if tw.IsActive(now) {
			return playlistID
		}
	}
	return ""
}
