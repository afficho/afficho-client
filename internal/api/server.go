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
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	r := chi.NewRouter()
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// ── Display renderer (loaded by Chromium) ────────────────────────────
	// These routes are intentionally unauthenticated — Chromium on the same
	// device needs to reach them without credentials.
	r.Get("/display", s.handleDisplay)
	r.Get("/display/current", s.handleDisplayCurrent)
	// TODO: WebSocket endpoint for push-based display control
	//   r.Get("/ws/display", s.handleDisplayWS)
	//   Messages: { type: "next" | "reload" | "alert", payload: ... }
	//   This enables: instant transitions, emergency alerts, live ticket queues, etc.

	// ── Admin UI ─────────────────────────────────────────────────────────
	// TODO: wrap these routes with requireAuth() middleware
	//   CE: HTTP Basic Auth with password from config.Admin.Password
	//   EE: handled upstream by Afficho Cloud web console (SSO/RBAC)
	r.Get("/", http.RedirectHandler("/admin", http.StatusFound).ServeHTTP)
	r.Get("/admin", s.handleAdmin)
	r.Get("/admin/*", s.handleAdmin)

	// ── REST API ─────────────────────────────────────────────────────────
	// TODO: apply requireAuth() to this entire route group
	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/status", s.handleStatus)

		r.Route("/content", func(r chi.Router) {
			r.Get("/", s.listContent)
			r.Post("/", s.createContent)      // add by URL or upload
			r.Get("/{id}", s.getContent)      // TODO: implement in Phase 4
			r.Patch("/{id}", s.updateContent) // TODO: implement in Phase 4
			r.Delete("/{id}", s.deleteContent)
		})

		r.Route("/playlists", func(r chi.Router) {
			r.Get("/", s.listPlaylists)
			r.Post("/", s.createPlaylist)
			r.Get("/{id}", s.getPlaylist)
			r.Put("/{id}/items", s.setPlaylistItems) // replace ordered item list
			r.Delete("/{id}", s.deletePlaylist)
			r.Post("/{id}/activate", s.activatePlaylist) // set as default
		})

		r.Get("/scheduler/status", s.handleSchedulerStatus)
		r.Post("/scheduler/next", s.handleSchedulerNext) // force advance (for testing)
	})

	// ── Static media files ───────────────────────────────────────────────
	// Served from the local media storage directory.
	r.Handle("/media/*", http.StripPrefix("/media/",
		http.FileServer(http.Dir(s.content.MediaDir())),
	))

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
