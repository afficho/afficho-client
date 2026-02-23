package api

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/afficho/afficho-client/internal/config"
	"github.com/afficho/afficho-client/internal/content"
	"github.com/afficho/afficho-client/internal/db"
	"github.com/afficho/afficho-client/internal/scheduler"
	"github.com/afficho/afficho-client/internal/updater"
	"github.com/afficho/afficho-client/web"
)

// Server is the HTTP API and admin UI server.
type Server struct {
	cfg       *config.Config
	db        *db.DB
	content   *content.Manager
	scheduler *scheduler.Scheduler
	updater   *updater.Updater
	hub       *Hub
	tpl       *adminTemplates
	mux       *chi.Mux
	version   string
	startedAt time.Time
}

// SetUpdater attaches the auto-updater to the server so update status
// can be exposed via the API. Must be called before Run.
func (s *Server) SetUpdater(u *updater.Updater) {
	s.updater = u
}

// Hub returns the WebSocket broadcast hub for sending messages to
// connected display clients. Used by the cloud connector to relay
// commands (reload, alert) from the cloud to the local display.
func (s *Server) Hub() *Hub {
	return s.hub
}

// NewServer wires up all routes and returns a ready-to-run Server.
func NewServer(
	cfg *config.Config,
	database *db.DB,
	mgr *content.Manager,
	sched *scheduler.Scheduler,
	version string,
) *Server {
	s := &Server{
		cfg:       cfg,
		db:        database,
		content:   mgr,
		scheduler: sched,
		hub:       newHub(),
		tpl:       initAdminTemplates(),
		version:   version,
		startedAt: time.Now(),
	}

	// Broadcast the current item to all display clients whenever the
	// scheduler advances or reloads the queue.
	sched.OnChange = s.BroadcastCurrent

	s.routes()
	return s
}

// securityHeaders adds baseline security headers to every response.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) routes() {
	r := chi.NewRouter()
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(securityHeaders)

	// ── Unauthenticated routes ───────────────────────────────────────────
	// Display renderer — Chromium on the local device must reach these
	// without credentials.
	r.Get("/display", s.handleDisplay)
	r.Get("/display/current", s.handleDisplayCurrent)
	r.Get("/display/settings", s.handleDisplaySettings)
	r.Post("/display/advance", s.handleDisplayAdvance)
	r.Get("/ws/display", s.handleDisplayWS)

	// Inline HTML content renderer (iframed by the display page).
	r.Get("/content/{id}/render", s.handleContentRender)

	// Static media files (with path traversal defense-in-depth).
	r.Route("/media", func(r chi.Router) {
		r.Use(safeMediaMiddleware)
		r.Handle("/*", http.StripPrefix("/media/",
			http.FileServer(http.Dir(s.content.MediaDir())),
		))
	})

	// Embedded static assets (CSS, JS, SVGs) — must be accessible for
	// the login page, so they live outside the auth group.
	staticSub, _ := fs.Sub(web.FS, "static")
	r.Handle("/static/*", http.StripPrefix("/static/",
		http.FileServer(http.FS(staticSub)),
	))

	// Login page — unauthenticated (renders its own form).
	r.Get("/admin/login", s.adminLoginPage)
	r.Post("/admin/login", s.adminLoginSubmit)

	// Read-only status endpoint (useful for monitoring / health checks).
	r.Get("/api/v1/status", s.handleStatus)

	// Health check endpoint (for watchdogs / load balancers).
	r.Get("/healthz", s.handleHealthz)

	// ── Authenticated routes ─────────────────────────────────────────────
	// Protected by requireAuth: skipped when admin password is empty,
	// otherwise HTTP Basic Auth + session cookie.
	r.Group(func(r chi.Router) {
		r.Use(s.requireAuth)

		// Admin UI
		r.Get("/", http.RedirectHandler("/admin", http.StatusFound).ServeHTTP)
		r.Get("/admin", s.adminDashboard)
		r.Get("/admin/content", s.adminContent)
		r.Post("/admin/content/add", s.adminContentAdd)
		r.Delete("/admin/content/{id}/delete", s.adminContentDelete)
		r.Get("/admin/playlists", s.adminPlaylists)
		r.Post("/admin/playlists/create", s.adminPlaylistCreate)
		r.Get("/admin/playlists/{id}", s.adminPlaylistEdit)
		r.Post("/admin/playlists/{id}/activate", s.adminPlaylistActivate)
		r.Delete("/admin/playlists/{id}/delete", s.adminPlaylistDelete)
		r.Get("/admin/schedules", s.adminSchedules)
		r.Post("/admin/schedules/create", s.adminScheduleCreate)
		r.Delete("/admin/schedules/{id}/delete", s.adminScheduleDelete)
		r.Post("/admin/display/settings", s.adminDisplaySettings)

		// REST API
		r.Route("/api/v1", func(r chi.Router) {
			r.Use(s.corsMiddleware)

			r.Route("/content", func(r chi.Router) {
				r.Get("/", s.listContent)
				r.With(middleware.Throttle(s.cfg.Security.UploadConcurrencyLimit)).Post("/", s.createContent)
				r.Get("/{id}", s.getContent)
				r.Patch("/{id}", s.updateContent)
				r.Delete("/{id}", s.deleteContent)
			})

			r.Route("/playlists", func(r chi.Router) {
				r.Get("/", s.listPlaylists)
				r.Post("/", s.createPlaylist)
				r.Get("/{id}", s.getPlaylist)
				r.Put("/{id}/items", s.setPlaylistItems)
				r.Delete("/{id}", s.deletePlaylist)
				r.Post("/{id}/activate", s.activatePlaylist)
			})

			r.Route("/schedules", func(r chi.Router) {
				r.Get("/", s.listSchedules)
				r.Post("/", s.createSchedule)
				r.Get("/{id}", s.getSchedule)
				r.Put("/{id}", s.updateSchedule)
				r.Delete("/{id}", s.deleteSchedule)
			})

			r.Get("/storage", s.handleStorageStatus)
			r.Get("/scheduler/status", s.handleSchedulerStatus)
			r.Post("/scheduler/next", s.handleSchedulerNext)
			r.Get("/system", s.handleSystemInfo)
			r.Get("/update/status", s.handleUpdateStatus)
			r.Post("/update/check", s.handleUpdateCheck)
		})
	})

	s.mux = r
}

// Run starts the HTTP server and blocks until ctx is cancelled.
func (s *Server) Run(ctx context.Context) error {
	srv := &http.Server{
		Addr:    s.cfg.ServerAddr(),
		Handler: s.mux,
	}

	go func() {
		<-ctx.Done()
		slog.Info("shutting down HTTP server")
		_ = srv.Shutdown(context.Background())
	}()

	slog.Info("HTTP server listening", "addr", s.cfg.ServerAddr())
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("http server: %w", err)
	}
	return nil
}
