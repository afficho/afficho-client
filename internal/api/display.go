package api

import (
	_ "embed"
	"encoding/json"
	"net/http"
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
