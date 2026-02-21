package api

import (
	"net/http"

	"github.com/afficho/afficho-client/internal/updater"
)

// handleUpdateStatus returns the current auto-updater state.
func (s *Server) handleUpdateStatus(w http.ResponseWriter, r *http.Request) {
	if s.updater == nil {
		respond(w, http.StatusOK, updater.Status{
			Enabled:        false,
			CurrentVersion: s.version,
		})
		return
	}
	respond(w, http.StatusOK, s.updater.Status())
}

// handleUpdateCheck triggers an immediate update check and returns the result.
func (s *Server) handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	if s.updater == nil {
		http.Error(w, "auto-update is disabled", http.StatusConflict)
		return
	}
	respond(w, http.StatusOK, s.updater.CheckNow())
}
