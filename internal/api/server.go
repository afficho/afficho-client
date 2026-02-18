package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/afficho/afficho-client/internal/config"
	"github.com/afficho/afficho-client/internal/content"
	"github.com/afficho/afficho-client/internal/db"
	"github.com/afficho/afficho-client/internal/scheduler"
)

// Server is the HTTP API and admin UI server.
type Server struct {
	cfg       *config.Config
	db        *db.DB
	content   *content.Manager
	scheduler *scheduler.Scheduler
	hub       *Hub
	mux       *chi.Mux
}

// NewServer wires up all routes and returns a ready-to-run Server.
func NewServer(
	cfg *config.Config,
	database *db.DB,
	mgr *content.Manager,
	sched *scheduler.Scheduler,
) *Server {
	s := &Server{
		cfg:       cfg,
		db:        database,
		content:   mgr,
		scheduler: sched,
		hub:       newHub(),
	}

	// Broadcast the current item to all display clients whenever the
	// scheduler advances or reloads the queue.
	sched.OnChange = s.BroadcastCurrent

	s.routes()
	return s
}

func (s *Server) routes() {
	r := chi.NewRouter()
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// ── Unauthenticated routes ───────────────────────────────────────────
	// Display renderer — Chromium on the local device must reach these
	// without credentials.
	r.Get("/display", s.handleDisplay)
	r.Get("/display/current", s.handleDisplayCurrent)
	r.Post("/display/advance", s.handleDisplayAdvance)
	r.Get("/ws/display", s.handleDisplayWS)

	// Inline HTML content renderer (iframed by the display page).
	r.Get("/content/{id}/render", s.handleContentRender)

	// Static media files.
	r.Handle("/media/*", http.StripPrefix("/media/",
		http.FileServer(http.Dir(s.content.MediaDir())),
	))

	// Read-only status endpoint (useful for monitoring / health checks).
	r.Get("/api/v1/status", s.handleStatus)

	// ── Authenticated routes ─────────────────────────────────────────────
	// Protected by requireAuth: skipped when admin password is empty,
	// otherwise HTTP Basic Auth + session cookie.
	r.Group(func(r chi.Router) {
		r.Use(s.requireAuth)

		// Admin UI
		r.Get("/", http.RedirectHandler("/admin", http.StatusFound).ServeHTTP)
		r.Get("/admin", s.handleAdmin)
		r.Get("/admin/*", s.handleAdmin)

		// REST API
		r.Route("/api/v1", func(r chi.Router) {
			r.Route("/content", func(r chi.Router) {
				r.Get("/", s.listContent)
				r.Post("/", s.createContent)
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

			r.Get("/storage", s.handleStorageStatus)
			r.Get("/scheduler/status", s.handleSchedulerStatus)
			r.Post("/scheduler/next", s.handleSchedulerNext)
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
