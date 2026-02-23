package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/afficho/afficho-client/internal/api"
	"github.com/afficho/afficho-client/internal/cloud"
	"github.com/afficho/afficho-client/internal/config"
	"github.com/afficho/afficho-client/internal/content"
	"github.com/afficho/afficho-client/internal/db"
	"github.com/afficho/afficho-client/internal/display"
	"github.com/afficho/afficho-client/internal/scheduler"
	"github.com/afficho/afficho-client/internal/updater"
)

// version and goarm are overridden at build time via ldflags.
var (
	version = "dev"
	goarm   = "" //nolint:gochecknoglobals // set via -ldflags for ARM builds
)

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
	server := api.NewServer(cfg, database, contentMgr, sched, version)

	// ── Context / signal handling ─────────────────────────────────────────
	ctx, cancel := signal.NotifyContext(context.Background(),
		syscall.SIGINT, syscall.SIGTERM,
	)
	defer cancel()

	// ── SIGHUP: reload config ────────────────────────────────────────────
	sighup := make(chan os.Signal, 1)
	signal.Notify(sighup, syscall.SIGHUP)
	go handleSIGHUP(sighup, *configPath, cfg, logLevel, sched)

	// ── Auto-updater ─────────────────────────────────────────────────────
	upd, updErr := updater.New(version, goarm, cfg)
	if updErr != nil {
		slog.Error("failed to initialise updater", "error", updErr)
	}
	if upd != nil {
		server.SetUpdater(upd)
		go upd.Run(ctx)
	}

	// ── Cloud connector ──────────────────────────────────────────────────
	if cfg.Cloud.Enabled {
		deviceID, err := database.DeviceID()
		if err != nil {
			slog.Error("failed to get device ID for cloud", "error", err)
		} else {
			cloudConn := cloud.New(cfg.Cloud, deviceID, version, cfg.Storage.DataDir)
			cloud.NewContentSyncer(cloudConn, database, contentMgr)
			cloud.NewPlaylistSyncer(cloudConn, database, sched)
			cloud.NewScheduleSyncer(cloudConn, database, sched)

			// Wrap the updater to satisfy cloud.UpdateTrigger (no return value).
			var updTrigger cloud.UpdateTrigger
			if upd != nil {
				updTrigger = updaterShim{upd}
			}
			cloud.NewCommandHandler(cloudConn, server.Hub(), updTrigger, deviceID)
			cloud.NewAlertHandler(cloudConn, server.Hub())

			// Proof-of-play logger: chain into scheduler's OnChange to
			// record item transitions, run flush loop in background.
			playLog := cloud.NewPlayLogger(cloudConn, database)
			prevOnChange := sched.OnChange
			sched.OnChange = func() {
				item, ok := sched.Current()
				itemID := ""
				if ok {
					itemID = item.ID
				}
				playLog.RecordTransition(itemID)
				if prevOnChange != nil {
					prevOnChange()
				}
			}
			go playLog.Run(ctx)

			// Flush pending proof-of-play records on every reconnect.
			cloudConn.OnConnect(playLog.Flush)

			// Expose cloud connector state for the status API.
			server.SetCloudConnector(cloudConn)
			server.SetPlayLogger(playLog)

			go cloudConn.Run(ctx)
		}
	}

	// ── Screen power schedule ─────────────────────────────────────────────
	if screenCtrl := display.NewScreenController(cfg); screenCtrl != nil {
		go screenCtrl.Run(ctx)
	}

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

// updaterShim adapts *updater.Updater (whose CheckNow returns Status)
// to the cloud.UpdateTrigger interface (CheckNow with no return).
type updaterShim struct{ u *updater.Updater }

func (s updaterShim) CheckNow() { s.u.CheckNow() }

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
		cfg.Security.CORSAllowedOrigins = newCfg.Security.CORSAllowedOrigins

		slog.Info("config reloaded successfully")

		// Trigger scheduler to pick up any DB changes that might
		// have happened alongside a config edit.
		sched.TriggerReload()
	}
}
