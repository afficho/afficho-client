package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/afficho/afficho-client/internal/content"
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

// contentItem is the JSON representation of a content_items row.
type contentItem struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Source      string `json:"source"`
	DurationS   int    `json:"duration_s"`
	SizeBytes   int64  `json:"size_bytes"`
	AllowPopups bool   `json:"allow_popups"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

func scanContentItem(row interface{ Scan(...any) error }) (contentItem, error) {
	var it contentItem
	var popups int
	err := row.Scan(&it.ID, &it.Name, &it.Type, &it.Source, &it.DurationS, &it.SizeBytes, &popups, &it.CreatedAt, &it.UpdatedAt)
	it.AllowPopups = popups != 0
	return it, err
}

const contentColumns = `id, name, type, source, duration_s, size_bytes, allow_popups, created_at, updated_at`

func (s *Server) listContent(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`SELECT ` + contentColumns + ` FROM content_items ORDER BY created_at DESC`)
	if err != nil {
		slog.Error("listing content", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	items := make([]contentItem, 0) // empty slice → [] in JSON, not null
	for rows.Next() {
		it, err := scanContentItem(rows)
		if err != nil {
			slog.Error("scanning content row", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		slog.Error("iterating content rows", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	respond(w, http.StatusOK, items)
}

type createContentReq struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	URL         string `json:"url"`
	HTML        string `json:"html"`
	DurationS   int    `json:"duration_s"`
	AllowPopups bool   `json:"allow_popups"`
}

// createContent dispatches to JSON or multipart handling based on Content-Type.
func (s *Server) createContent(w http.ResponseWriter, r *http.Request) {
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/form-data") {
		s.createContentUpload(w, r)
		return
	}
	s.createContentJSON(w, r)
}

// createContentJSON handles JSON-based content creation (url, image/video by URL, html).
func (s *Server) createContentJSON(w http.ResponseWriter, r *http.Request) {
	var req createContentReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	if req.DurationS <= 0 {
		http.Error(w, "duration_s must be > 0", http.StatusBadRequest)
		return
	}

	id := uuid.New().String()
	popups := 0
	if req.AllowPopups {
		popups = 1
	}

	var source string
	var sizeBytes int64

	switch req.Type {
	case "url":
		if err := validateURL(req.URL); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		source = req.URL

	case "image", "video":
		if err := validateURL(req.URL); err != nil {
			http.Error(w, "url is required for image/video content: "+err.Error(), http.StatusBadRequest)
			return
		}
		localPath, size, err := s.content.DownloadMedia(id, req.URL, req.Type)
		if err != nil {
			slog.Error("downloading media", "error", err)
			http.Error(w, "failed to download media: "+err.Error(), http.StatusBadRequest)
			return
		}
		source = "/media/" + filepath.Base(localPath)
		sizeBytes = size

	case "html":
		if strings.TrimSpace(req.HTML) == "" {
			http.Error(w, "html field is required for type html", http.StatusBadRequest)
			return
		}
		// Store the HTML in the source column; serve via /content/{id}/render.
		source = req.HTML

	default:
		http.Error(w, `type must be "url", "image", "video", or "html"`, http.StatusBadRequest)
		return
	}

	_, err := s.db.Exec(`
		INSERT INTO content_items (id, name, type, source, duration_s, size_bytes, allow_popups)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, req.Name, req.Type, source, req.DurationS, sizeBytes, popups,
	)
	if err != nil {
		slog.Error("creating content", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	it, err := s.fetchContentItem(id)
	if err != nil {
		slog.Error("fetching created content", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	s.scheduler.TriggerReload()
	s.BroadcastCurrent()
	respond(w, http.StatusCreated, it)
}

// createContentUpload handles multipart file upload for image/video content.
func (s *Server) createContentUpload(w http.ResponseWriter, r *http.Request) {
	maxBytes := s.content.MaxUploadBytes()
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes+1024*1024) // headroom for form fields

	if err := r.ParseMultipartForm(maxBytes); err != nil {
		http.Error(w, "request too large or invalid multipart form", http.StatusBadRequest)
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	contentType := r.FormValue("type")
	if contentType != "image" && contentType != "video" {
		http.Error(w, `type must be "image" or "video" for file uploads`, http.StatusBadRequest)
		return
	}
	durationS, err := strconv.Atoi(r.FormValue("duration_s"))
	if err != nil || durationS <= 0 {
		http.Error(w, "duration_s must be a positive integer", http.StatusBadRequest)
		return
	}
	allowPopups := r.FormValue("allow_popups") == "true"

	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "file is required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Validate magic bytes.
	_, ext, err := content.ValidateMediaType(file, contentType)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	id := uuid.New().String()
	localPath, size, err := s.content.SaveUpload(id, file, ext, maxBytes)
	if err != nil {
		slog.Error("saving upload", "error", err)
		http.Error(w, "failed to save file: "+err.Error(), http.StatusBadRequest)
		return
	}

	popups := 0
	if allowPopups {
		popups = 1
	}

	_, err = s.db.Exec(`
		INSERT INTO content_items (id, name, type, source, duration_s, size_bytes, allow_popups)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, name, contentType, "/media/"+filepath.Base(localPath), durationS, size, popups,
	)
	if err != nil {
		_ = s.content.Delete(localPath)
		slog.Error("creating content", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	it, err := s.fetchContentItem(id)
	if err != nil {
		slog.Error("fetching created content", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	s.scheduler.TriggerReload()
	s.BroadcastCurrent()
	respond(w, http.StatusCreated, it)
}

func (s *Server) getContent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	it, err := s.fetchContentItem(id)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("getting content", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	respond(w, http.StatusOK, it)
}

type updateContentReq struct {
	Name        *string `json:"name"`
	URL         *string `json:"url"`
	DurationS   *int    `json:"duration_s"`
	AllowPopups *bool   `json:"allow_popups"`
}

func (s *Server) updateContent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	existing, err := s.fetchContentItem(id)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("fetching content for update", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	var req updateContentReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	// Merge provided fields into existing values.
	name := existing.Name
	source := existing.Source
	durationS := existing.DurationS
	allowPopups := existing.AllowPopups

	if req.Name != nil {
		name = strings.TrimSpace(*req.Name)
		if name == "" {
			http.Error(w, "name must not be empty", http.StatusBadRequest)
			return
		}
	}
	if req.URL != nil {
		if err := validateURL(*req.URL); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		source = *req.URL
	}
	if req.DurationS != nil {
		durationS = *req.DurationS
		if durationS <= 0 {
			http.Error(w, "duration_s must be > 0", http.StatusBadRequest)
			return
		}
	}
	if req.AllowPopups != nil {
		allowPopups = *req.AllowPopups
	}

	popups := 0
	if allowPopups {
		popups = 1
	}

	_, err = s.db.Exec(`
		UPDATE content_items
		SET name = ?, source = ?, duration_s = ?, allow_popups = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`,
		name, source, durationS, popups, id,
	)
	if err != nil {
		slog.Error("updating content", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	updated, err := s.fetchContentItem(id)
	if err != nil {
		slog.Error("fetching updated content", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	s.scheduler.TriggerReload()
	s.BroadcastCurrent()
	respond(w, http.StatusOK, updated)
}

func (s *Server) deleteContent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	// Fetch the item first so we can clean up local media after deletion.
	existing, err := s.fetchContentItem(id)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("fetching content for delete", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if _, err := s.db.Exec(`DELETE FROM content_items WHERE id = ?`, id); err != nil {
		slog.Error("deleting content", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Clean up local media file if applicable.
	if existing.Type == "image" || existing.Type == "video" {
		mediaPath := filepath.Join(s.content.MediaDir(), filepath.Base(existing.Source))
		if err := s.content.Delete(mediaPath); err != nil {
			slog.Warn("failed to delete media file", "path", mediaPath, "error", err)
		}
	}

	s.scheduler.TriggerReload()
	s.BroadcastCurrent()
	respond(w, http.StatusNoContent, nil)
}

// fetchContentItem loads a single content item by ID.
func (s *Server) fetchContentItem(id string) (contentItem, error) {
	row := s.db.QueryRow(`SELECT `+contentColumns+` FROM content_items WHERE id = ?`, id)
	return scanContentItem(row)
}

// validateURL checks that raw is a valid http or https URL.
func validateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return errors.New("invalid URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.New("URL must use http or https scheme")
	}
	if u.Host == "" {
		return errors.New("URL must have a host")
	}
	return nil
}

// ── Storage ──────────────────────────────────────────────────────────────────

func (s *Server) handleStorageStatus(w http.ResponseWriter, r *http.Request) {
	var count int
	var usedBytes int64
	err := s.db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(size_bytes), 0) FROM content_items`).Scan(&count, &usedBytes)
	if err != nil {
		slog.Error("querying storage stats", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	respond(w, http.StatusOK, map[string]any{
		"item_count":       count,
		"used_bytes":       usedBytes,
		"max_cache_bytes":  int64(s.cfg.Storage.MaxCacheGB) * 1024 * 1024 * 1024,
		"max_upload_bytes": s.content.MaxUploadBytes(),
	})
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
