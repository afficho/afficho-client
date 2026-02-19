package api

import (
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
)

//go:embed display.html
var displayHTML []byte

// handleDisplay serves the fullscreen display page loaded by Chromium.
func (s *Server) handleDisplay(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(displayHTML)
}

// handleDisplayCurrent returns the currently active content item as JSON.
// The display page polls this endpoint to know what to render and for how long.
func (s *Server) handleDisplayCurrent(w http.ResponseWriter, r *http.Request) {
	item, ok := s.scheduler.Current()
	if !ok {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(item)
}

// handleDisplayAdvance advances the scheduler to the next item. Unauthenticated
// because the display page calls it when a video finishes before the duration timer.
func (s *Server) handleDisplayAdvance(w http.ResponseWriter, r *http.Request) {
	s.scheduler.Advance()
	w.WriteHeader(http.StatusNoContent)
}

// handleContentRender serves inline HTML content items for iframe embedding.
// Unauthenticated — the display page needs to load these.
func (s *Server) handleContentRender(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var contentType, source string
	err := s.db.QueryRow(`SELECT type, source FROM content_items WHERE id = ?`, id).Scan(&contentType, &source)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if contentType != "html" {
		http.Error(w, "not an HTML content item", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(source))
}

// handleDisplaySettings returns display preferences from device_meta.
// Unauthenticated — the display page fetches this on boot.
func (s *Server) handleDisplaySettings(w http.ResponseWriter, r *http.Request) {
	var val string
	err := s.db.QueryRow(`SELECT value FROM device_meta WHERE key = 'show_progress_bar'`).Scan(&val)
	if err != nil {
		val = "false"
	}
	respond(w, http.StatusOK, map[string]any{
		"show_progress_bar": val == "true",
	})
}
