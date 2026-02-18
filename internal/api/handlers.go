package api

import (
	"encoding/json"
	"net/http"
)

// ── Status ────────────────────────────────────────────────────────────────────

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	current, hasCurrent := s.scheduler.Current()
	respond(w, http.StatusOK, map[string]any{
		"status":      "ok",
		"has_content": hasCurrent,
		"current":     current,
		"queue_len":   len(s.scheduler.Queue()),
	})
}

// ── Scheduler ─────────────────────────────────────────────────────────────────

func (s *Server) handleSchedulerStatus(w http.ResponseWriter, r *http.Request) {
	current, _ := s.scheduler.Current()
	respond(w, http.StatusOK, map[string]any{
		"current": current,
		"queue":   s.scheduler.Queue(),
	})
}

func (s *Server) handleSchedulerNext(w http.ResponseWriter, r *http.Request) {
	next, ok := s.scheduler.Advance()
	if !ok {
		respond(w, http.StatusNoContent, nil)
		return
	}
	respond(w, http.StatusOK, next)
}

// ── Content ───────────────────────────────────────────────────────────────────

// TODO: Implement full content CRUD
func (s *Server) listContent(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}

func (s *Server) createContent(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}

func (s *Server) getContent(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}

func (s *Server) updateContent(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}

func (s *Server) deleteContent(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}

// ── Playlists ─────────────────────────────────────────────────────────────────

// TODO: Implement full playlist CRUD
func (s *Server) listPlaylists(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}

func (s *Server) createPlaylist(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}

func (s *Server) getPlaylist(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}

func (s *Server) setPlaylistItems(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}

func (s *Server) deletePlaylist(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}

func (s *Server) activatePlaylist(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}

// ── Admin UI ──────────────────────────────────────────────────────────────────

// TODO: Implement admin UI (server-side rendered HTML)
func (s *Server) handleAdmin(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "admin UI not yet implemented — use the REST API", http.StatusNotImplemented)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func respond(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if body != nil {
		_ = json.NewEncoder(w).Encode(body)
	}
}
