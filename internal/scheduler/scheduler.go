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
	ID        string `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"`   // "image" | "video" | "url" | "html"
	Source    string `json:"source"` // local /media/... path or external URL
	DurationS int    `json:"duration_s"`
}

// Scheduler manages the active playlist and tracks the current playback position.
type Scheduler struct {
	db      *db.DB
	content *content.Manager

	mu      sync.RWMutex
	queue   []Item
	current int

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
	item := s.queue[s.current]
	s.mu.Unlock()

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
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.reloadQueue(); err != nil {
				slog.Error("scheduler: queue reload failed", "error", err)
			}
		case <-s.reload:
			if err := s.reloadQueue(); err != nil {
				slog.Error("scheduler: queue reload failed", "error", err)
			}
		}
	}
}

// reloadQueue queries the database for the active playlist and rebuilds the queue.
func (s *Scheduler) reloadQueue() error {
	items, err := s.loadDefaultPlaylist()
	if err != nil {
		return err
	}

	s.mu.Lock()
	// Preserve current position if the queue grew; reset if it shrank or changed.
	if s.current >= len(items) {
		s.current = 0
	}
	s.queue = items
	s.mu.Unlock()

	slog.Debug("scheduler: queue reloaded", "items", len(items))
	s.notifyChange()
	return nil
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
			COALESCE(pi.duration_override_s, ci.duration_s) AS duration_s
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
		if err := rows.Scan(&it.ID, &it.Name, &it.Type, &it.Source, &it.DurationS); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, rows.Err()
}
