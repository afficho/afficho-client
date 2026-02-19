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
	"github.com/afficho/afficho-client/internal/scheduler"
)

// ── Status ────────────────────────────────────────────────────────────────────

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	current, hasCurrent := s.scheduler.Current()
	respond(w, http.StatusOK, map[string]any{
		"status":      "ok",
		"version":     s.version,
		"has_content": hasCurrent,
		"current":     current,
		"queue_len":   len(s.scheduler.Queue()),
	})
}

// ── Scheduler ─────────────────────────────────────────────────────────────────

func (s *Server) handleSchedulerStatus(w http.ResponseWriter, r *http.Request) {
	current, ok := s.scheduler.Current()
	activePlaylist := s.scheduler.ActivePlaylistID()
	respond(w, http.StatusOK, map[string]any{
		"current":            current,
		"has_current":        ok,
		"queue":              s.scheduler.Queue(),
		"seconds_until_next": s.scheduler.SecondsUntilNext(),
		"active_playlist_id": activePlaylist,
		"using_schedule":     activePlaylist != "",
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
		req.DurationS = 10
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
	durationS, _ := strconv.Atoi(r.FormValue("duration_s"))
	if durationS <= 0 {
		durationS = 10
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

type playlistSummary struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	IsDefault bool   `json:"is_default"`
	ItemCount int    `json:"item_count"`
	CreatedAt string `json:"created_at"`
}

type playlistDetail struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	IsDefault bool           `json:"is_default"`
	CreatedAt string         `json:"created_at"`
	Items     []playlistItem `json:"items"`
}

type playlistItem struct {
	ID                string `json:"id"`
	ContentID         string `json:"content_id"`
	ContentName       string `json:"content_name"`
	ContentType       string `json:"content_type"`
	Position          int    `json:"position"`
	DurationS         *int   `json:"duration_s"`
	DurationOverrideS *int   `json:"duration_override_s"`
	ContentDurationS  int    `json:"content_duration_s"`
}

func (s *Server) listPlaylists(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`
		SELECT p.id, p.name, p.is_default, p.created_at, COUNT(pi.id)
		FROM playlists p
		LEFT JOIN playlist_items pi ON pi.playlist_id = p.id
		GROUP BY p.id
		ORDER BY p.created_at ASC`)
	if err != nil {
		slog.Error("listing playlists", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	items := make([]playlistSummary, 0)
	for rows.Next() {
		var p playlistSummary
		var def int
		if err := rows.Scan(&p.ID, &p.Name, &def, &p.CreatedAt, &p.ItemCount); err != nil {
			slog.Error("scanning playlist row", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		p.IsDefault = def != 0
		items = append(items, p)
	}
	if err := rows.Err(); err != nil {
		slog.Error("iterating playlist rows", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	respond(w, http.StatusOK, items)
}

func (s *Server) createPlaylist(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	id := uuid.New().String()
	_, err := s.db.Exec(`INSERT INTO playlists (id, name, is_default) VALUES (?, ?, 0)`, id, req.Name)
	if err != nil {
		slog.Error("creating playlist", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	p, err := s.fetchPlaylistSummary(id)
	if err != nil {
		slog.Error("fetching created playlist", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	s.scheduler.TriggerReload()
	s.BroadcastCurrent()
	respond(w, http.StatusCreated, p)
}

func (s *Server) getPlaylist(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	p, err := s.fetchPlaylistDetail(id)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("getting playlist", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	respond(w, http.StatusOK, p)
}

func (s *Server) setPlaylistItems(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	// Verify playlist exists.
	var exists int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM playlists WHERE id = ?`, id).Scan(&exists); err != nil {
		slog.Error("checking playlist", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if exists == 0 {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	var items []struct {
		ContentID         string `json:"content_id"`
		DurationS         *int   `json:"duration_s"`
		DurationOverrideS *int   `json:"duration_override_s"`
	}
	if err := json.NewDecoder(r.Body).Decode(&items); err != nil {
		http.Error(w, "invalid JSON body: expected array of items", http.StatusBadRequest)
		return
	}

	// Validate all content IDs exist.
	for _, item := range items {
		var found int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM content_items WHERE id = ?`, item.ContentID).Scan(&found); err != nil {
			slog.Error("checking content_id", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if found == 0 {
			http.Error(w, "content_id not found: "+item.ContentID, http.StatusBadRequest)
			return
		}
	}

	tx, err := s.db.Begin()
	if err != nil {
		slog.Error("beginning transaction", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if _, err := tx.Exec(`DELETE FROM playlist_items WHERE playlist_id = ?`, id); err != nil {
		_ = tx.Rollback()
		slog.Error("clearing playlist items", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	for i, item := range items {
		piID := uuid.New().String()
		// Accept duration_s (preferred) or legacy duration_override_s.
		dur := item.DurationS
		if dur == nil {
			dur = item.DurationOverrideS
		}
		_, err := tx.Exec(`
			INSERT INTO playlist_items (id, playlist_id, content_id, position, duration_override_s)
			VALUES (?, ?, ?, ?, ?)`,
			piID, id, item.ContentID, i, dur,
		)
		if err != nil {
			_ = tx.Rollback()
			slog.Error("inserting playlist item", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}

	if err := tx.Commit(); err != nil {
		slog.Error("committing playlist items", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	p, err := s.fetchPlaylistDetail(id)
	if err != nil {
		slog.Error("fetching updated playlist", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	s.scheduler.TriggerReload()
	s.BroadcastCurrent()
	respond(w, http.StatusOK, p)
}

func (s *Server) deletePlaylist(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var name string
	var isDefault int
	err := s.db.QueryRow(`SELECT name, is_default FROM playlists WHERE id = ?`, id).Scan(&name, &isDefault)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("fetching playlist for delete", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if isDefault != 0 {
		http.Error(w, "cannot delete the default playlist", http.StatusBadRequest)
		return
	}

	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM playlists`).Scan(&total); err != nil {
		slog.Error("counting playlists", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if total <= 1 {
		http.Error(w, "cannot delete the last playlist", http.StatusBadRequest)
		return
	}

	if _, err := s.db.Exec(`DELETE FROM playlists WHERE id = ?`, id); err != nil {
		slog.Error("deleting playlist", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	s.scheduler.TriggerReload()
	s.BroadcastCurrent()
	respond(w, http.StatusNoContent, nil)
}

func (s *Server) activatePlaylist(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var exists int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM playlists WHERE id = ?`, id).Scan(&exists); err != nil {
		slog.Error("checking playlist", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if exists == 0 {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	tx, err := s.db.Begin()
	if err != nil {
		slog.Error("beginning transaction", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if _, err := tx.Exec(`UPDATE playlists SET is_default = 0 WHERE is_default = 1`); err != nil {
		_ = tx.Rollback()
		slog.Error("deactivating current playlist", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if _, err := tx.Exec(`UPDATE playlists SET is_default = 1 WHERE id = ?`, id); err != nil {
		_ = tx.Rollback()
		slog.Error("activating playlist", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		slog.Error("committing playlist activation", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	p, err := s.fetchPlaylistDetail(id)
	if err != nil {
		slog.Error("fetching activated playlist", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	s.scheduler.TriggerReload()
	s.BroadcastCurrent()
	respond(w, http.StatusOK, p)
}

// fetchPlaylistSummary loads a playlist with its item count.
func (s *Server) fetchPlaylistSummary(id string) (playlistSummary, error) {
	var p playlistSummary
	var def int
	err := s.db.QueryRow(`
		SELECT p.id, p.name, p.is_default, p.created_at, COUNT(pi.id)
		FROM playlists p
		LEFT JOIN playlist_items pi ON pi.playlist_id = p.id
		WHERE p.id = ?
		GROUP BY p.id`, id).Scan(&p.ID, &p.Name, &def, &p.CreatedAt, &p.ItemCount)
	p.IsDefault = def != 0
	return p, err
}

// fetchPlaylistDetail loads a playlist with its ordered items.
func (s *Server) fetchPlaylistDetail(id string) (playlistDetail, error) {
	var p playlistDetail
	var def int
	err := s.db.QueryRow(`SELECT id, name, is_default, created_at FROM playlists WHERE id = ?`, id).
		Scan(&p.ID, &p.Name, &def, &p.CreatedAt)
	if err != nil {
		return p, err
	}
	p.IsDefault = def != 0

	rows, err := s.db.Query(`
		SELECT pi.id, pi.content_id, ci.name, ci.type, pi.position,
		       pi.duration_override_s, ci.duration_s
		FROM playlist_items pi
		JOIN content_items ci ON ci.id = pi.content_id
		WHERE pi.playlist_id = ?
		ORDER BY pi.position ASC`, id)
	if err != nil {
		return p, err
	}
	defer rows.Close()

	p.Items = make([]playlistItem, 0)
	for rows.Next() {
		var item playlistItem
		if err := rows.Scan(&item.ID, &item.ContentID, &item.ContentName, &item.ContentType, &item.Position, &item.DurationOverrideS, &item.ContentDurationS); err != nil {
			return p, err
		}
		item.DurationS = item.DurationOverrideS
		p.Items = append(p.Items, item)
	}
	return p, rows.Err()
}

// ── Schedules ─────────────────────────────────────────────────────────────────

type scheduleItem struct {
	ID         string `json:"id"`
	PlaylistID string `json:"playlist_id"`
	CronExpr   string `json:"cron_expr"`
	Priority   int    `json:"priority"`
	CreatedAt  string `json:"created_at"`
}

func (s *Server) listSchedules(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`SELECT id, playlist_id, cron_expr, priority, created_at FROM schedules ORDER BY priority DESC`)
	if err != nil {
		slog.Error("listing schedules", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	items := make([]scheduleItem, 0)
	for rows.Next() {
		var it scheduleItem
		if err := rows.Scan(&it.ID, &it.PlaylistID, &it.CronExpr, &it.Priority, &it.CreatedAt); err != nil {
			slog.Error("scanning schedule row", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		slog.Error("iterating schedule rows", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	respond(w, http.StatusOK, items)
}

func (s *Server) createSchedule(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PlaylistID string `json:"playlist_id"`
		CronExpr   string `json:"cron_expr"`
		Priority   int    `json:"priority"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	// Validate playlist exists.
	var exists int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM playlists WHERE id = ?`, req.PlaylistID).Scan(&exists); err != nil {
		slog.Error("checking playlist", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if exists == 0 {
		http.Error(w, "playlist_id not found", http.StatusBadRequest)
		return
	}

	// Validate cron expression.
	if _, err := scheduler.ParseTimeWindow(req.CronExpr); err != nil {
		http.Error(w, "invalid cron_expr: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.Priority < 0 {
		http.Error(w, "priority must be >= 0", http.StatusBadRequest)
		return
	}

	id := uuid.New().String()
	_, err := s.db.Exec(`INSERT INTO schedules (id, playlist_id, cron_expr, priority) VALUES (?, ?, ?, ?)`,
		id, req.PlaylistID, req.CronExpr, req.Priority,
	)
	if err != nil {
		slog.Error("creating schedule", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	it, err := s.fetchSchedule(id)
	if err != nil {
		slog.Error("fetching created schedule", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	s.scheduler.TriggerReload()
	s.BroadcastCurrent()
	respond(w, http.StatusCreated, it)
}

func (s *Server) getSchedule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	it, err := s.fetchSchedule(id)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("getting schedule", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	respond(w, http.StatusOK, it)
}

func (s *Server) updateSchedule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	existing, err := s.fetchSchedule(id)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("fetching schedule for update", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	var req struct {
		PlaylistID *string `json:"playlist_id"`
		CronExpr   *string `json:"cron_expr"`
		Priority   *int    `json:"priority"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	playlistID := existing.PlaylistID
	cronExpr := existing.CronExpr
	priority := existing.Priority

	if req.PlaylistID != nil {
		var exists int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM playlists WHERE id = ?`, *req.PlaylistID).Scan(&exists); err != nil {
			slog.Error("checking playlist", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if exists == 0 {
			http.Error(w, "playlist_id not found", http.StatusBadRequest)
			return
		}
		playlistID = *req.PlaylistID
	}
	if req.CronExpr != nil {
		if _, err := scheduler.ParseTimeWindow(*req.CronExpr); err != nil {
			http.Error(w, "invalid cron_expr: "+err.Error(), http.StatusBadRequest)
			return
		}
		cronExpr = *req.CronExpr
	}
	if req.Priority != nil {
		if *req.Priority < 0 {
			http.Error(w, "priority must be >= 0", http.StatusBadRequest)
			return
		}
		priority = *req.Priority
	}

	_, err = s.db.Exec(`UPDATE schedules SET playlist_id = ?, cron_expr = ?, priority = ? WHERE id = ?`,
		playlistID, cronExpr, priority, id,
	)
	if err != nil {
		slog.Error("updating schedule", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	updated, err := s.fetchSchedule(id)
	if err != nil {
		slog.Error("fetching updated schedule", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	s.scheduler.TriggerReload()
	s.BroadcastCurrent()
	respond(w, http.StatusOK, updated)
}

func (s *Server) deleteSchedule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var exists int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM schedules WHERE id = ?`, id).Scan(&exists); err != nil {
		slog.Error("checking schedule", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if exists == 0 {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	if _, err := s.db.Exec(`DELETE FROM schedules WHERE id = ?`, id); err != nil {
		slog.Error("deleting schedule", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	s.scheduler.TriggerReload()
	s.BroadcastCurrent()
	respond(w, http.StatusNoContent, nil)
}

func (s *Server) fetchSchedule(id string) (scheduleItem, error) {
	var it scheduleItem
	err := s.db.QueryRow(`SELECT id, playlist_id, cron_expr, priority, created_at FROM schedules WHERE id = ?`, id).
		Scan(&it.ID, &it.PlaylistID, &it.CronExpr, &it.Priority, &it.CreatedAt)
	return it, err
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func respond(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if body != nil {
		_ = json.NewEncoder(w).Encode(body)
	}
}
