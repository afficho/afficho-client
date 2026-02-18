package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/afficho/afficho-client/internal/api"
	"github.com/afficho/afficho-client/internal/config"
	"github.com/afficho/afficho-client/internal/content"
	"github.com/afficho/afficho-client/internal/db"
	"github.com/afficho/afficho-client/internal/display"
	"github.com/afficho/afficho-client/internal/scheduler"
)

// version is overridden at build time via -ldflags "-X main.version=v1.2.3".
var version = "dev"

func main() {
	configPath := flag.String("config", "/etc/afficho/config.toml", "path to config file")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		os.Stdout.WriteString("afficho-client " + version + "\n")
		return
	}

	// ── Load config ───────────────────────────────────────────────────────
	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// ── Logger ────────────────────────────────────────────────────────────
	logLevel := &slog.LevelVar{}
	if cfg.Logging.Debug {
		logLevel.Set(slog.LevelDebug)
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	})))

	slog.Info("starting afficho-client", "version", version, "config", *configPath)

	// ── Database ──────────────────────────────────────────────────────────
	database, err := db.Open(cfg.Storage.DataDir)
	if err != nil {
		slog.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	// ── Content manager ───────────────────────────────────────────────────
	contentMgr := content.NewManager(cfg, database)
	if err := contentMgr.Init(); err != nil {
		_ = database.Close()
		slog.Error("failed to initialise content manager", "error", err)
		os.Exit(1) //nolint:gocritic // database explicitly closed above
	}

	// ── Scheduler ─────────────────────────────────────────────────────────
	sched := scheduler.New(database, contentMgr)

	// ── HTTP server ───────────────────────────────────────────────────────
	server := api.NewServer(cfg, database, contentMgr, sched)

	// ── Context / signal handling ─────────────────────────────────────────
	ctx, cancel := signal.NotifyContext(context.Background(),
		syscall.SIGINT, syscall.SIGTERM,
	)
	defer cancel()

	// ── SIGHUP: reload config ────────────────────────────────────────────
	sighup := make(chan os.Signal, 1)
	signal.Notify(sighup, syscall.SIGHUP)
	go handleSIGHUP(sighup, *configPath, cfg, logLevel, sched)

	// ── Browser ───────────────────────────────────────────────────────────
	if cfg.Display.LaunchBrowser {
		browser := display.New(cfg)
		go browser.Launch(ctx)
	}

	// ── Start services ────────────────────────────────────────────────────
	go sched.Run(ctx)

	if err := server.Run(ctx); err != nil {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}

	slog.Info("afficho-client stopped cleanly")
}

// handleSIGHUP listens for SIGHUP and reloads the config file. It updates
// the live config pointer, adjusts the log level, and triggers a scheduler
// refresh. Fields that require a restart (server address, data directory,
// display settings) are logged but not applied until the next restart.
func handleSIGHUP(
	ch <-chan os.Signal,
	path string,
	cfg *config.Config,
	logLevel *slog.LevelVar,
	sched *scheduler.Scheduler,
) {
	for range ch {
		slog.Info("received SIGHUP, reloading config", "path", path)

		newCfg, err := config.Load(path)
		if err != nil {
			slog.Error("config reload failed, keeping current config", "error", err)
			continue
		}

		// Update log level immediately.
		if newCfg.Logging.Debug {
			logLevel.Set(slog.LevelDebug)
		} else {
			logLevel.Set(slog.LevelInfo)
		}

		// Update hot-reloadable fields in the shared config.
		cfg.Admin.Password = newCfg.Admin.Password
		cfg.Logging.Debug = newCfg.Logging.Debug
		cfg.Storage.MaxCacheGB = newCfg.Storage.MaxCacheGB

		slog.Info("config reloaded successfully")

		// Trigger scheduler to pick up any DB changes that might
		// have happened alongside a config edit.
		sched.TriggerReload()
	}
}
