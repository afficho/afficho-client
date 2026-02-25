package api

import (
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	types "github.com/afficho/afficho-types"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/afficho/afficho-client/internal/content"
	"github.com/afficho/afficho-client/internal/scheduler"
	"github.com/afficho/afficho-client/web"
)

// adminTemplates holds pre-parsed page templates.
type adminTemplates struct {
	login        *template.Template
	dashboard    *template.Template
	contentPage  *template.Template
	playlists    *template.Template
	playlistEdit *template.Template
	schedules    *template.Template
}

// Template helper functions.
var templateFuncs = template.FuncMap{
	"formatBytes": formatBytes,
	"truncate":    truncate,
	"typeIcon":    typeIcon,
	"add":         func(a, b int) int { return a + b },
	"derefInt": func(p *int) int {
		if p != nil {
			return *p
		}
		return 0
	},
}

func parsePageTemplate(name string) *template.Template {
	return template.Must(
		template.New("layout").Funcs(templateFuncs).ParseFS(
			web.FS,
			"templates/layout.html",
			"templates/"+name,
		),
	)
}

func initAdminTemplates() *adminTemplates {
	return &adminTemplates{
		login:        template.Must(template.New("login.html").Funcs(templateFuncs).ParseFS(web.FS, "templates/login.html")),
		dashboard:    parsePageTemplate("dashboard.html"),
		contentPage:  parsePageTemplate("content.html"),
		playlists:    parsePageTemplate("playlists.html"),
		playlistEdit: parsePageTemplate("playlist_edit.html"),
		schedules:    parsePageTemplate("schedules.html"),
	}
}

// pageData is the base data passed to every layout template.
type pageData struct {
	ActiveNav string
	Flash     string
	FlashType string // "success" or "error"
}

func newPageData(r *http.Request, nav string) pageData {
	d := pageData{ActiveNav: nav}
	if f := r.URL.Query().Get("flash"); f != "" {
		d.FlashType = f
		d.Flash = r.URL.Query().Get("msg")
		if d.Flash == "" {
			if f == "success" {
				d.Flash = "Done!"
			} else {
				d.Flash = "Something went wrong."
			}
		}
	}
	return d
}

func (s *Server) renderPage(w http.ResponseWriter, tmpl *template.Template, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "layout", data); err != nil {
		slog.Error("rendering template", "error", err)
	}
}

// ── Login ─────────────────────────────────────────────────────────────────────

func (s *Server) adminLoginPage(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Admin.Password == "" {
		http.Redirect(w, r, "/admin", http.StatusFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = s.tpl.login.Execute(w, map[string]string{})
}

func (s *Server) adminLoginSubmit(w http.ResponseWriter, r *http.Request) {
	password := s.cfg.Admin.Password
	if password == "" {
		http.Redirect(w, r, "/admin", http.StatusFound)
		return
	}

	given := r.FormValue("password")
	if subtle.ConstantTimeCompare([]byte(given), []byte(password)) != 1 {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = s.tpl.login.Execute(w, map[string]string{"Error": "Invalid password"})
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    s.signSession(password),
		Path:     "/",
		MaxAge:   int(sessionMaxAge.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/admin", http.StatusFound)
}

// ── Dashboard ─────────────────────────────────────────────────────────────────

func (s *Server) adminDashboard(w http.ResponseWriter, r *http.Request) {
	current, _ := s.scheduler.Current()
	queue := s.scheduler.Queue()
	activePlaylistID := s.scheduler.ActivePlaylistID()

	// Find current index in queue.
	currentIndex := 0
	for i, item := range queue {
		if item.ID == current.ID {
			currentIndex = i
			break
		}
	}

	// Resolve active playlist name.
	activePlaylistName := "Default"
	if activePlaylistID != "" {
		var name string
		if err := s.db.QueryRow(`SELECT name FROM playlists WHERE id = ?`, activePlaylistID).Scan(&name); err == nil {
			activePlaylistName = name
		}
	} else {
		var name string
		if err := s.db.QueryRow(`SELECT name FROM playlists WHERE is_default = 1`).Scan(&name); err == nil {
			activePlaylistName = name
		}
	}

	var contentCount int
	var usedBytes int64
	_ = s.db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(size_bytes), 0) FROM content_items`).Scan(&contentCount, &usedBytes)

	var progressVal string
	if err := s.db.QueryRow(`SELECT value FROM device_meta WHERE key = 'show_progress_bar'`).Scan(&progressVal); err != nil {
		progressVal = "false"
	}

	data := struct {
		pageData
		Current            scheduler.Item
		Queue              []scheduler.Item
		CurrentIndex       int
		ActivePlaylistName string
		UsingSchedule      bool
		SecondsUntilNext   float64
		ContentCount       int
		UsedBytes          int64
		ConnectedDisplays  int
		ShowProgressBar    bool
	}{
		pageData:           newPageData(r, "dashboard"),
		Current:            current,
		Queue:              queue,
		CurrentIndex:       currentIndex,
		ActivePlaylistName: activePlaylistName,
		UsingSchedule:      activePlaylistID != "",
		SecondsUntilNext:   s.scheduler.SecondsUntilNext(),
		ContentCount:       contentCount,
		UsedBytes:          usedBytes,
		ConnectedDisplays:  s.hub.count(),
		ShowProgressBar:    progressVal == "true",
	}
	s.renderPage(w, s.tpl.dashboard, data)
}

// ── Content ───────────────────────────────────────────────────────────────────

func (s *Server) adminContent(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`SELECT ` + contentColumns + ` FROM content_items ORDER BY created_at DESC`)
	if err != nil {
		slog.Error("admin: listing content", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var items []contentItem
	for rows.Next() {
		it, err := scanContentItem(rows)
		if err != nil {
			slog.Error("admin: scanning content row", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		slog.Error("admin: iterating content rows", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	data := struct {
		pageData
		Items []contentItem
	}{
		pageData: newPageData(r, "content"),
		Items:    items,
	}
	s.renderPage(w, s.tpl.contentPage, data)
}

func (s *Server) adminContentAdd(w http.ResponseWriter, r *http.Request) {
	// The HTML form always sends multipart/form-data (file input present),
	// so dispatch based on whether a file was actually provided, not the
	// Content-Type header.
	maxBytes := s.content.MaxUploadBytes()
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes+1024*1024)
	_ = r.ParseMultipartForm(maxBytes)

	_, _, fileErr := r.FormFile("file")
	contentType := r.FormValue("type")

	if fileErr == nil && (contentType == "image" || contentType == "video") {
		s.adminContentAddUpload(w, r)
		return
	}
	s.adminContentAddForm(w, r)
}

func (s *Server) adminContentAddForm(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.FormValue("name"))
	contentType := r.FormValue("type")
	urlVal := r.FormValue("url")
	htmlVal := r.FormValue("html")
	durationStr := r.FormValue("duration_s")
	allowPopups := r.FormValue("allow_popups") == "true"

	if name == "" {
		s.adminRedirect(w, r, "/admin/content", "error", "Name is required")
		return
	}

	durationS, _ := strconv.Atoi(durationStr)
	if durationS <= 0 {
		durationS = 10
	}

	id := newUUID()
	popups := 0
	if allowPopups {
		popups = 1
	}

	var source string
	var sizeBytes int64

	switch contentType {
	case "url":
		if err := validateURL(urlVal); err != nil {
			s.adminRedirect(w, r, "/admin/content", "error", "Invalid URL: "+err.Error())
			return
		}
		source = urlVal
	case "image", "video":
		if urlVal != "" {
			if err := validateURL(urlVal); err != nil {
				s.adminRedirect(w, r, "/admin/content", "error", "Invalid URL: "+err.Error())
				return
			}
			localPath, size, err := s.content.DownloadMedia(id, urlVal, contentType)
			if err != nil {
				s.adminRedirect(w, r, "/admin/content", "error", "Download failed: "+err.Error())
				return
			}
			source = "/media/" + filepath.Base(localPath)
			sizeBytes = size
		} else {
			s.adminRedirect(w, r, "/admin/content", "error", "URL or file is required for "+contentType)
			return
		}
	case "html":
		if strings.TrimSpace(htmlVal) == "" {
			s.adminRedirect(w, r, "/admin/content", "error", "HTML content is required")
			return
		}
		source = htmlVal
	default:
		s.adminRedirect(w, r, "/admin/content", "error", "Invalid content type")
		return
	}

	_, err := s.db.Exec(`
		INSERT INTO content_items (id, name, type, source, duration_s, size_bytes, allow_popups)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, name, contentType, source, durationS, sizeBytes, popups,
	)
	if err != nil {
		slog.Error("admin: creating content", "error", err)
		s.adminRedirect(w, r, "/admin/content", "error", "Failed to create content")
		return
	}

	s.scheduler.TriggerReload()
	s.BroadcastCurrent()
	s.adminRedirect(w, r, "/admin/content", "success", "Content created")
}

// adminContentAddUpload handles file uploads for image/video content.
// The multipart form is already parsed by adminContentAdd.
func (s *Server) adminContentAddUpload(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		s.adminRedirect(w, r, "/admin/content", "error", "Name is required")
		return
	}

	contentType := r.FormValue("type")

	durationS, _ := strconv.Atoi(r.FormValue("duration_s"))
	if durationS <= 0 {
		durationS = 10
	}

	allowPopups := r.FormValue("allow_popups") == "true"

	file, _, err := r.FormFile("file")
	if err != nil {
		s.adminRedirect(w, r, "/admin/content", "error", "File is required for upload")
		return
	}
	defer file.Close()

	_, ext, err := content.ValidateMediaType(file, contentType)
	if err != nil {
		s.adminRedirect(w, r, "/admin/content", "error", err.Error())
		return
	}

	id := newUUID()
	localPath, size, err := s.content.SaveUpload(id, file.(io.ReadSeeker), ext, s.content.MaxUploadBytes())
	if err != nil {
		s.adminRedirect(w, r, "/admin/content", "error", "Failed to save file: "+err.Error())
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
		s.adminRedirect(w, r, "/admin/content", "error", "Failed to create content")
		return
	}

	s.scheduler.TriggerReload()
	s.BroadcastCurrent()
	s.adminRedirect(w, r, "/admin/content", "success", "Content uploaded")
}

func (s *Server) adminContentDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	existing, err := s.fetchContentItem(id)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	if _, err := s.db.Exec(`DELETE FROM content_items WHERE id = ?`, id); err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	if existing.Type == "image" || existing.Type == "video" {
		mediaPath := filepath.Join(s.content.MediaDir(), filepath.Base(existing.Source))
		if err := s.content.Delete(mediaPath); err != nil {
			slog.Warn("admin: failed to delete media file", "path", mediaPath, "error", err)
		}
	}

	s.scheduler.TriggerReload()
	s.BroadcastCurrent()
	w.WriteHeader(http.StatusOK)
}

// ── Playlists ─────────────────────────────────────────────────────────────────

func (s *Server) adminPlaylists(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`
		SELECT p.id, p.name, p.is_default, p.created_at, COUNT(pi.id)
		FROM playlists p
		LEFT JOIN playlist_items pi ON pi.playlist_id = p.id
		GROUP BY p.id
		ORDER BY p.created_at ASC`)
	if err != nil {
		slog.Error("admin: listing playlists", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var playlists []playlistSummary
	for rows.Next() {
		var p playlistSummary
		var def int
		if err := rows.Scan(&p.ID, &p.Name, &def, &p.CreatedAt, &p.ItemCount); err != nil {
			slog.Error("admin: scanning playlist row", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		p.IsDefault = def != 0
		playlists = append(playlists, p)
	}

	data := struct {
		pageData
		Playlists []playlistSummary
	}{
		pageData:  newPageData(r, "playlists"),
		Playlists: playlists,
	}
	s.renderPage(w, s.tpl.playlists, data)
}

func (s *Server) adminPlaylistCreate(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		s.adminRedirect(w, r, "/admin/playlists", "error", "Name is required")
		return
	}

	id := newUUID()
	_, err := s.db.Exec(`INSERT INTO playlists (id, name, is_default) VALUES (?, ?, 0)`, id, name)
	if err != nil {
		slog.Error("admin: creating playlist", "error", err)
		s.adminRedirect(w, r, "/admin/playlists", "error", "Failed to create playlist")
		return
	}

	s.scheduler.TriggerReload()
	s.BroadcastCurrent()
	s.adminRedirect(w, r, "/admin/playlists", "success", "Playlist created")
}

func (s *Server) adminPlaylistEdit(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	playlist, err := s.fetchPlaylistDetail(id)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("admin: fetching playlist", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Load all content for the "add item" dropdown.
	contentRows, err := s.db.Query(`SELECT ` + contentColumns + ` FROM content_items ORDER BY name ASC`)
	if err != nil {
		slog.Error("admin: listing content for playlist editor", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer contentRows.Close()

	var allContent []contentItem
	for contentRows.Next() {
		it, err := scanContentItem(contentRows)
		if err != nil {
			slog.Error("admin: scanning content row", "error", err)
			continue
		}
		allContent = append(allContent, it)
	}

	data := struct {
		pageData
		Playlist   playlistDetail
		AllContent []contentItem
	}{
		pageData:   newPageData(r, "playlists"),
		Playlist:   playlist,
		AllContent: allContent,
	}
	s.renderPage(w, s.tpl.playlistEdit, data)
}

func (s *Server) adminPlaylistActivate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var exists int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM playlists WHERE id = ?`, id).Scan(&exists); err != nil || exists == 0 {
		s.adminRedirect(w, r, "/admin/playlists", "error", "Playlist not found")
		return
	}

	tx, err := s.db.Begin()
	if err != nil {
		s.adminRedirect(w, r, "/admin/playlists", "error", "Internal error")
		return
	}

	if _, err := tx.Exec(`UPDATE playlists SET is_default = 0 WHERE is_default = 1`); err != nil {
		_ = tx.Rollback()
		s.adminRedirect(w, r, "/admin/playlists", "error", "Internal error")
		return
	}
	if _, err := tx.Exec(`UPDATE playlists SET is_default = 1 WHERE id = ?`, id); err != nil {
		_ = tx.Rollback()
		s.adminRedirect(w, r, "/admin/playlists", "error", "Internal error")
		return
	}
	if err := tx.Commit(); err != nil {
		s.adminRedirect(w, r, "/admin/playlists", "error", "Internal error")
		return
	}

	s.scheduler.TriggerReload()
	s.BroadcastCurrent()
	s.adminRedirect(w, r, "/admin/playlists", "success", "Default playlist updated")
}

func (s *Server) adminPlaylistDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var isDefault int
	err := s.db.QueryRow(`SELECT is_default FROM playlists WHERE id = ?`, id).Scan(&isDefault)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "", http.StatusNotFound)
		return
	}
	if isDefault != 0 {
		http.Error(w, "Cannot delete default playlist", http.StatusBadRequest)
		return
	}

	if _, err := s.db.Exec(`DELETE FROM playlists WHERE id = ?`, id); err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	s.scheduler.TriggerReload()
	s.BroadcastCurrent()
	w.WriteHeader(http.StatusOK)
}

// ── Schedules ─────────────────────────────────────────────────────────────────

// scheduleView is the display-friendly version of a schedule for templates.
type scheduleView struct {
	ID           string
	PlaylistName string
	TimeRange    string
	Days         string
	Priority     int
}

func (s *Server) adminSchedules(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`
		SELECT sc.id, sc.cron_expr, sc.priority, p.name
		FROM schedules sc
		JOIN playlists p ON p.id = sc.playlist_id
		ORDER BY sc.priority DESC`)
	if err != nil {
		slog.Error("admin: listing schedules", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var schedules []scheduleView
	for rows.Next() {
		var sv scheduleView
		var cronExpr string
		if err := rows.Scan(&sv.ID, &cronExpr, &sv.Priority, &sv.PlaylistName); err != nil {
			slog.Error("admin: scanning schedule row", "error", err)
			continue
		}
		tw, err := scheduler.ParseTimeWindow(cronExpr)
		if err != nil {
			sv.TimeRange = cronExpr
			sv.Days = "?"
		} else {
			sv.TimeRange = fmt.Sprintf("%02d:%02d-%02d:%02d", tw.StartHour, tw.StartMin, tw.EndHour, tw.EndMin)
			sv.Days = tw.Days
		}
		schedules = append(schedules, sv)
	}

	// Load playlists for the create form dropdown.
	playlistRows, err := s.db.Query(`SELECT id, name, is_default FROM playlists ORDER BY name ASC`)
	if err != nil {
		slog.Error("admin: listing playlists for schedule form", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer playlistRows.Close()

	var playlists []playlistSummary
	for playlistRows.Next() {
		var p playlistSummary
		var def int
		if err := playlistRows.Scan(&p.ID, &p.Name, &def); err != nil {
			continue
		}
		p.IsDefault = def != 0
		playlists = append(playlists, p)
	}

	data := struct {
		pageData
		Schedules []scheduleView
		Playlists []playlistSummary
	}{
		pageData:  newPageData(r, "schedules"),
		Schedules: schedules,
		Playlists: playlists,
	}
	s.renderPage(w, s.tpl.schedules, data)
}

func (s *Server) adminScheduleCreate(w http.ResponseWriter, r *http.Request) {
	playlistID := r.FormValue("playlist_id")
	startTime := r.FormValue("start_time")
	endTime := r.FormValue("end_time")
	days := r.FormValue("days")
	priorityStr := r.FormValue("priority")

	if playlistID == "" {
		s.adminRedirect(w, r, "/admin/schedules", "error", "Playlist is required")
		return
	}

	cronExpr := startTime + "-" + endTime + " " + days
	if _, err := scheduler.ParseTimeWindow(cronExpr); err != nil {
		s.adminRedirect(w, r, "/admin/schedules", "error", "Invalid time window: "+err.Error())
		return
	}

	priority, err := strconv.Atoi(priorityStr)
	if err != nil || priority < 0 {
		s.adminRedirect(w, r, "/admin/schedules", "error", "Priority must be >= 0")
		return
	}

	id := newUUID()
	_, err = s.db.Exec(`INSERT INTO schedules (id, playlist_id, cron_expr, priority) VALUES (?, ?, ?, ?)`,
		id, playlistID, cronExpr, priority,
	)
	if err != nil {
		slog.Error("admin: creating schedule", "error", err)
		s.adminRedirect(w, r, "/admin/schedules", "error", "Failed to create schedule")
		return
	}

	s.scheduler.TriggerReload()
	s.BroadcastCurrent()
	s.adminRedirect(w, r, "/admin/schedules", "success", "Schedule created")
}

func (s *Server) adminScheduleDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var exists int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM schedules WHERE id = ?`, id).Scan(&exists); err != nil || exists == 0 {
		http.Error(w, "", http.StatusNotFound)
		return
	}

	if _, err := s.db.Exec(`DELETE FROM schedules WHERE id = ?`, id); err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	s.scheduler.TriggerReload()
	s.BroadcastCurrent()
	w.WriteHeader(http.StatusOK)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func (s *Server) adminRedirect(w http.ResponseWriter, r *http.Request, path, flash, msg string) {
	http.Redirect(w, r, path+"?flash="+flash+"&msg="+template.URLQueryEscaper(msg), http.StatusFound)
}

func newUUID() string {
	return uuid.New().String()
}

func formatBytes(b int64) string {
	const (
		kb = 1024
		mb = kb * 1024
		gb = mb * 1024
	)
	switch {
	case b >= gb:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(gb))
	case b >= mb:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(mb))
	case b >= kb:
		return fmt.Sprintf("%.0f KB", float64(b)/float64(kb))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func typeIcon(t string) string {
	switch t {
	case "image":
		return "\U0001F5BC" // framed picture
	case "video":
		return "\U0001F3AC" // clapper board
	case "url":
		return "\U0001F310" // globe
	case "html":
		return "\U0001F4C4" // page
	default:
		return "\U00002753" // question mark
	}
}

// ── Emergency alerts ─────────────────────────────────────────────────────────

// adminAlertSend broadcasts an alert to all connected display clients.
func (s *Server) adminAlertSend(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	text := strings.TrimSpace(r.FormValue("text"))
	urgency := r.FormValue("urgency")
	ttlStr := r.FormValue("ttl_s")

	if text == "" {
		s.adminRedirect(w, r, "/admin", "error", "Alert text is required")
		return
	}

	switch urgency {
	case "info", "warning", "critical":
		// valid
	default:
		urgency = "warning"
	}

	ttl, _ := strconv.Atoi(ttlStr)
	if ttl < 0 {
		ttl = 30
	}

	alert := types.AlertMessage{Text: text, TTLS: ttl, Urgency: urgency}
	payload, err := json.Marshal(alert)
	if err != nil {
		slog.Error("admin: marshalling alert", "error", err)
		s.adminRedirect(w, r, "/admin", "error", "Failed to send alert")
		return
	}

	s.hub.Broadcast(types.WSMessage{Type: types.TypeAlert, Payload: payload})
	slog.Info("admin: alert sent", "text", text, "urgency", urgency, "ttl_s", ttl)
	s.adminRedirect(w, r, "/admin", "success", "Alert sent to displays")
}

// adminAlertClear broadcasts a clear_alert to all connected display clients.
func (s *Server) adminAlertClear(w http.ResponseWriter, r *http.Request) {
	s.hub.Broadcast(types.WSMessage{Type: types.TypeClearAlert, Payload: json.RawMessage(`{}`)})
	slog.Info("admin: alert cleared")
	s.adminRedirect(w, r, "/admin", "success", "Alert cleared")
}

// ── Display settings ────────────────────────────────────────────────────────

// adminDisplaySettings toggles display preferences (progress bar) and broadcasts
// the change to all connected display clients.
func (s *Server) adminDisplaySettings(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	enabled := r.FormValue("show_progress_bar") == "on"
	val := "false"
	if enabled {
		val = "true"
	}

	_, err := s.db.Exec(
		`INSERT INTO device_meta (key, value) VALUES ('show_progress_bar', ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, val)
	if err != nil {
		slog.Error("admin: saving display settings", "error", err)
		s.adminRedirect(w, r, "/admin", "error", "Failed to save display settings")
		return
	}

	// Broadcast to all live display clients so they update in real time.
	if payload, err := json.Marshal(map[string]any{"show_progress_bar": enabled}); err == nil {
		s.hub.Broadcast(types.WSMessage{Type: types.TypeSettings, Payload: payload})
	}

	s.adminRedirect(w, r, "/admin", "success", "Display settings saved")
}
