# Afficho Client — Claude Context

## What this is

Go daemon for the **Afficho** open-source digital signage platform.
Runs on Raspberry Pi / Linux. Manages content, drives a Chromium kiosk via HTTP,
and exposes a local admin UI + REST API.

See `TODOS.md` for all planned work organized by phase.

## Environment

- **Always inside the devcontainer** — do not suggest running `go` commands on the host.
- Go 1.24, chi v5, modernc/sqlite (pure-Go, no CGo), BurntSushi/toml.
- Dev server: `make dev` → http://localhost:8080

## Project layout

```
cmd/afficho/main.go          Entry point — wires all services, handles signals
internal/config/             TOML config loading with defaults
internal/db/                 SQLite open + schema (WAL mode, FK on)
internal/content/            Local media storage and download
internal/scheduler/          Playlist queue — Current() / Advance() / TriggerReload()
internal/display/            Chromium kiosk launcher (restarts on crash)
internal/api/
  server.go                  chi router, graceful shutdown
  display.go                 GET /display (HTML page) + GET /display/current (JSON)
  handlers.go                All other endpoints (many are stubs — see TODOs)
```

## Key architectural decisions

- **Chromium is a dumb renderer.** It polls `/display/current` today; WebSocket
  (`/ws/display`) is the planned replacement (Phase 3 in TODOS.md).
- **WebSocket message envelope:** `{ "type": "current|reload|alert|ticket", "payload": {} }`
  — same format will be used by cloud sync so the display page doesn't care whether
  control is local or cloud-originated.
- **SQLite, single writer.** `db.SetMaxOpenConns(1)`. Don't add connection pooling.
- **CE vs EE split:** single config password is CE. SSO/RBAC lives in the Afficho Cloud
  web console (EE), not in this repo.
- **`/display` and `/ws/display` are intentionally unauthenticated** — Chromium on the
  same device calls them without credentials. Auth (Phase 2) wraps `/admin` and
  `/api/v1` only.

## Common commands (run inside the container)

```bash
make dev          # run with config.example.toml (no browser launch)
make test         # go test -race ./...
make lint         # golangci-lint run ./...
make build-all    # cross-compile amd64 / arm64 / armv7 / armv6
go mod tidy       # after adding/removing imports
```

## Conventions

- Structured logging via `log/slog` — use `slog.Info/Error/Debug`, not `fmt.Println`.
- Return errors wrapped with `fmt.Errorf("context: %w", err)`; don't log and return.
- After any content or playlist write: call `scheduler.TriggerReload()` and (later)
  broadcast a WebSocket `current` message.
- All JSON responses go through the `respond(w, code, body)` helper in `handlers.go`.
- Keep handler files thin — business logic belongs in `internal/` packages.
- Corporate design: the canonical style guide lives in `../afficho-brand/Styleguide.md`.
  The local `Styleguide.md` is a pointer. Brand assets (icon.svg, logo.svg) in
  `web/static/` are copies — update from `../afficho-brand/assets/` when they change.