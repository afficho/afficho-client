# Afficho Client — Implementation TODOs

Tracks all pending work. Update this file as features land.

**Legend:** `[ ]` pending · `[~]` in progress · `[x]` done

---

## Edition model

| Feature | CE (this repo) | EE (Afficho Cloud) |
|---|---|---|
| Admin auth | Single password via config | SSO + RBAC |
| Device management | Local only | Web console |
| Display control | WebSocket (local) | Cloud-pushed via WebSocket |
| Scheduling | Local cron | Cloud-managed |

---

## Phase 1 — Core Infrastructure

- [x] Project layout (`cmd/`, `internal/`, `web/`)
- [x] TOML config with defaults + file override
- [x] `[admin]` password field in config (CE auth)
- [x] SQLite schema (content, playlists, playlist_items, schedules)
- [x] HTTP server (chi router, graceful shutdown)
- [x] Structured logging (`log/slog`)
- [x] Signal handling (SIGINT, SIGTERM)
- [x] Multi-arch build (amd64, arm64, armv7, armv6)
- [x] Dev container (Go 1.23, golangci-lint, forwarded port 8080)
- [x] GitHub Actions CI (build + lint + cross-compile on every push)
- [x] GitHub Actions release (matrix build → GitHub release + checksums)
- [x] Makefile with build / test / lint / dev targets
- [x] Database migration versioning (replace `CREATE IF NOT EXISTS` with numbered migrations)
- [x] SIGHUP handler: reload config + trigger scheduler refresh without restart
- [x] Embed static assets with `//go:embed` (currently inline strings)
- [x] `.golangci.yml` linter config file
- [x] `LICENSE` file (Apache 2.0)

---

## Phase 2 — Admin Authentication (CE)

Simple single-password protection for the admin UI and all write API endpoints.
EE authentication (SSO, RBAC) lives in the Afficho Cloud web console, not here.

- [x] `requireAuth()` chi middleware using HTTP Basic Auth
  - Check `config.Admin.Password`; skip auth entirely if password is empty
  - Unauthenticated requests: return `401` with `WWW-Authenticate: Basic` header
- [x] Apply `requireAuth()` to `/admin`, `/admin/*`, and all `/api/v1` write routes
  - `/display`, `/display/current`, `/ws/display`, and `/media/*` stay unauthenticated
    (Chromium on the local device must reach them without credentials)
- [x] Expose `GET /api/v1/status` unauthenticated (read-only, useful for monitoring)
- [x] Display a login prompt in the browser when no session cookie is present
- [x] Session cookie: issue a short-lived signed token after successful auth to avoid
  re-prompting on every page load (use `crypto/hmac` + config password as key)
- [x] Document the security model: CE password is for local-network protection only;
  do not expose port 8080 to the internet without a reverse proxy + TLS

---

## Phase 3 — WebSocket Display Control

WebSocket replaces the JS polling approach for display transitions. It also forms
the foundation for future real-time features (emergency alerts, ticket queues, etc.).

### Server-side
- [x] WebSocket endpoint `GET /ws/display` (upgrade via `github.com/coder/websocket`)
- [x] Hub: track all connected display clients (fan-out broadcaster)
- [x] Message types (JSON envelope `{ "type": "...", "payload": { ... } }`):
  - `current` — sent on connect and on every item change: full `Item` object
  - `reload`  — tell display page to reload itself (after software update)
  - `alert`   — overlay an urgent message on screen (text + optional TTL)
  - *(future)* `ticket` — push a ticket/queue entry onto the display
  - *(future)* `clear_alert` — dismiss an active alert
- [x] Broadcast `current` message whenever the scheduler advances
- [x] Broadcast `current` message immediately after any playlist/content change
- [x] Reconnect handling: send `current` on every new WebSocket connection so a
  freshly opened display page gets the right content without waiting

### Client-side (display page)
- [x] Replace JS polling loop with WebSocket connection to `/ws/display`
- [x] Reconnect with exponential back-off on disconnect (Chromium restart, network blip)
- [x] Fall back to polling `/display/current` if WebSocket fails after N retries
- [x] Handle `alert` message: show full-screen overlay with message text + auto-dismiss
- [x] Handle `reload` message: call `location.reload()`

---

## Phase 4 — Content Management: Web Pages (v1)

First content type: web pages rendered in a full-screen iframe. This covers the
most common digital signage use case (dashboards, live websites, internal portals).

### REST endpoints
- [x] `GET  /api/v1/content` — list all items (id, name, type, url, duration_s, created_at)
- [x] `POST /api/v1/content` — add a URL content item
  ```json
  { "name": "...", "type": "url", "url": "https://...", "duration_s": 30 }
  ```
- [x] `GET    /api/v1/content/{id}` — get single item
- [x] `PATCH  /api/v1/content/{id}` — update name / url / duration
- [x] `DELETE /api/v1/content/{id}` — remove item (also removes from all playlists)
- [x] After any write: call `scheduler.TriggerReload()` + broadcast WebSocket `current`

### Validation
- [x] Require `https://` or `http://` scheme
- [x] Reject obviously invalid URLs (use `url.Parse`)
- [x] `duration_s` must be > 0

### iframe sandboxing
- [x] Default sandbox: `allow-scripts allow-same-origin allow-forms`
- [x] Per-item `allow_popups` flag for content that needs it
- [x] Document known limitations (X-Frame-Options, CSP on external sites)

---

## Phase 5 — Content Management: Images & Video

Images and video are stored locally. The daemon downloads them on add; Chromium
loads them from `/media/`.

### Images
- [x] `POST /api/v1/content` with `type: "image"`:
  - Accept external URL → download to `data/media/{id}.{ext}`
  - Accept multipart file upload
- [x] Accepted MIME types: `image/jpeg`, `image/png`, `image/gif`, `image/webp`
- [x] Validate magic bytes (not just Content-Type / extension)
- [x] Reject files > configurable size limit (`storage.max_upload_mb`)
- [x] Store `size_bytes` in DB

### Video
- [x] `POST /api/v1/content` with `type: "video"`:
  - Accept external URL → download
  - Accept multipart upload
- [x] Accepted MIME types: `video/mp4`, `video/webm`
- [x] Validate magic bytes
- [x] Display page: advance on `video.ended` event (before duration timer)
- [x] Display page: `autoplay`, `muted`, `playsinline` attributes

### Storage management
- [x] Track total media size; expose in `GET /api/v1/storage`
- [ ] Cache eviction when `storage.max_cache_gb` is exceeded (LRU — delete items
  not in any active playlist first, then oldest by last-played)

### Content type: inline HTML
- [x] `type: "html"` — store an HTML string in the DB, serve it via
  `/content/{id}/render` and iframe it
- [x] Use case: custom slides built with a future editor

---

## Phase 6 — Playlist Management API

- [x] `GET  /api/v1/playlists` — list playlists (id, name, is_default, item count)
- [x] `POST /api/v1/playlists` — create playlist `{ "name": "..." }`
- [x] `GET  /api/v1/playlists/{id}` — get playlist with full ordered item list
- [x] `PUT  /api/v1/playlists/{id}/items` — replace item list
  ```json
  [{ "content_id": "...", "duration_override_s": null }, ...]
  ```
- [x] `DELETE /api/v1/playlists/{id}` — delete (prevent deleting the last playlist)
- [x] `POST /api/v1/playlists/{id}/activate` — set as default (deactivates previous)
- [x] Auto-create a default playlist named "Default" on first run (migration 3)
- [x] After any change: `scheduler.TriggerReload()` + broadcast WebSocket `current`

---

## Phase 7 — Scheduler

- [x] Basic queue: `Current()` / `Advance()` / `Queue()`
- [x] Periodic DB reload (every 30 s)
- [x] `TriggerReload()` for instant refresh after writes
- [x] Server-side advance timer: track expiry of current item, auto-call `Advance()`
  and broadcast WebSocket `current` — removes reliance on client-side timing
- [x] `GET /api/v1/scheduler/status` — current item, queue, seconds until next advance
- [x] Cron-based schedule: activate playlist X during time window Y
  - Simple `HH:MM–HH:MM weekdays/weekends/everyday` syntax (implemented)
  - Evaluate `robfig/cron` for full cron expression support (future)
- [x] Schedule priority: higher-priority schedule overrides lower at the same time
- [x] Handle empty playlist gracefully (WebSocket sends `null` current; display shows splash)

---

## Phase 8 — Admin UI

### Functionality
- [x] Login page (shown when password is set and no valid session cookie exists)
- [x] Dashboard: current item preview (iframe), queue list, device status strip
- [x] Content library: card grid — URL items show favicon + URL, images show thumbnail,
  video shows thumbnail + duration badge
- [x] Add URL form: name, URL, duration (seconds)
- [x] Delete content with confirmation dialog
- [x] Playlist editor: drag-to-reorder items, per-item duration override inline
- [x] Playlist switcher: create new / activate existing
- [x] Storage stats: used / total, item count
- [x] Live current-item preview auto-refreshes via WebSocket (reuses `/ws/display`)

### Technical choices
- [x] Server-side rendered templates (`html/template`) + HTMX for dynamic parts
  - No build step, no JS framework, works well with Go
- [x] Embed templates + static assets with `//go:embed web/`
- [x] Responsive layout (usable on a phone for on-site content management)
- [x] Flash messages (success / error) via cookie or query param

---

## Phase 8.6 — Display improvements

### Flash prevention
- [x] Double-buffer rendering: two layers (`layer-a` / `layer-b`), swap on load
- [x] Same-item skip: if the incoming item ID matches the current one, skip re-render
- [x] Timeout fallback: swap after 5s if iframe `load` never fires

### Progress bar
- [x] Thin gradient bar (3px) at viewport bottom, CSS animation over `duration_s`
- [x] Setting stored in `device_meta` (`show_progress_bar` key)
- [x] `GET /display/settings` — unauthenticated endpoint for display page boot
- [x] `POST /admin/display/settings` — admin toggle on the dashboard
- [x] WebSocket `settings` message — live update without page reload

---

## Phase 8.5 — Duration belongs to playlists, not content

Duration is primarily a **playlist-level** concern: the same image may need 5s in
one rotation and 30s in another. Content items keep a default duration as a
fallback, but it should not be a required decision at creation time.

### Backend
- [x] Make `duration_s` on content items default to 10s automatically — stop
  requiring it in the `POST /api/v1/content` endpoint (treat 0 / missing as
  "use default 10s")
- [x] Rename the playlist_items column concept: `duration_override_s` → the
  **primary** duration; the content-level value is only the "fallback"
- [x] Scheduler: when building the queue, prefer `playlist_items.duration_override_s`;
  only fall back to `content_items.duration_s` if the override is NULL
  (already works via `COALESCE` — confirmed)

### Admin UI
- [x] Content creation form: make duration optional, pre-filled with "10",
  labelled "Default duration (seconds)" with help text "Can be overridden
  per-playlist"
- [x] Playlist editor: make the duration column prominent (not a secondary
  override input) — label it "Duration (s)", pre-fill with the content's
  default, let the user change it per-item
- [x] Show effective duration in the dashboard queue list (resolved value
  via scheduler's COALESCE — already correct)

### REST API
- [x] `PUT /api/v1/playlists/{id}/items` — accept `duration_s` (rename from
  `duration_override_s` in the JSON contract) for clarity; backend still
  stores in `duration_override_s` column; legacy field still accepted

---

## Phase 9 — Hardware & OS Integration

- [x] `systemd` service unit file (`deploy/afficho.service`)
- [x] Install script (`scripts/install.sh`): copy binary, create service, write config
- [x] Disable screensaver/DPMS on launch:
  `xset s off && xset -dpms && xset s noblank`
- [x] Screen power schedule: turn HDMI off at night, on in the morning
  - Use `vcgencmd display_power` (RPi) or `xset dpms force off/on`
  - Config: `screen_off_time` / `screen_on_time` in `[display]` (HH:MM format)
- [x] System info endpoint `GET /api/v1/system`:
  CPU temp, memory usage, disk usage, uptime, local IP, afficho version
- [x] Health check endpoint `GET /healthz` (200 OK, for watchdog / load balancer)
- [x] Log rotation config (`deploy/logrotate.d/afficho`)
- [x] Wayland support in browser launcher (`--ozone-platform=wayland`)
  - Config: `platform = "auto" | "x11" | "wayland"` in `[display]`
- [x] Auto-detect browser executable (try in order: chromium-browser, chromium,
  google-chrome, brave-browser) — set `browser = "auto"` in config

---

## Phase 10 — Cloud Sync (Afficho Cloud / EE)

- [ ] Generate stable device ID on first run (UUIDv4, stored in `device_meta`)
- [ ] Device registration: POST device info (ID, hostname, IP, arch, version) to cloud
- [ ] Heartbeat: periodic POST with current status (playing item, uptime, storage)
- [ ] Receive updates from cloud: new playlist, new content URLs, config changes
- [ ] Content download: fetch from cloud-provided signed URLs → local media cache
- [ ] Auth token storage in `device_meta`, refresh before expiry
- [ ] Offline resilience: operate fully from local DB when cloud is unreachable
- [ ] `GET /api/v1/cloud/status` — device ID, last sync, connection state
- [ ] Protocol: WebSocket (persistent, bidirectional) — reuse the message envelope
  from Phase 3; cloud sends same `current` / `alert` / `reload` messages

---

## Phase 11 — Security

- [x] Auth design decided: CE = single config password, EE = cloud console SSO/RBAC
- [ ] Implement `requireAuth()` middleware (see Phase 2)
- [ ] CORS: restrict `/api/v1` to same origin by default; configurable allowlist
- [ ] Rate limiting on upload endpoint (prevent disk fill)
- [ ] Path traversal audit on `/media/` file server
- [ ] iframe sandbox policy review per content type (Phase 4–5)
- [ ] Optional TLS: document reverse-proxy setup (nginx / Caddy) for HTTPS
- [ ] Security note in README: do not expose port 8080 to the internet unprotected

---

## Phase 12 — Testing

- [ ] Unit: config loading (defaults, file override, missing file)
- [ ] Unit: scheduler queue logic (advance, wrap-around, empty queue, reload)
- [ ] Unit: content manager path traversal guard
- [ ] Unit: `requireAuth` middleware (with / without password, valid / invalid token)
- [ ] Integration: REST API via `net/http/httptest`
  - Add URL → create playlist → activate → GET `/display/current` → correct item
- [ ] Integration: WebSocket — connect, receive `current`, trigger advance, receive new `current`
- [ ] Integration: graceful shutdown under in-flight requests
- [ ] Benchmark: scheduler `Current()` under concurrent read load

---

## Phase 13 — Packaging & Distribution

- [ ] Use goreleaser for semantic versioning
- [ ] `.deb` package (using `nfpm`) for Raspberry Pi OS / Debian
- [ ] Docker image (`FROM debian:bookworm-slim`) for dev/testing
- [ ] Docker Compose file: daemon + Chromium in headless mode (for CI display tests)
- [ ] Raspberry Pi OS image recipe (pi-gen) for zero-setup SD card flashing
- [ ] Auto-update: check GitHub releases API, download, verify checksum, replace binary

---

## Backlog / Nice to Have

- [ ] **Emergency alert overlay** — cloud-pushed message that pre-empts all content,
  shown as a banner or full-screen takeover (WebSocket `alert` message, Phase 3)
- [ ] **Ticket / queue display** — push structured data (e.g. customer number) to the
  screen via WebSocket `ticket` message; rendered as an HTML overlay
- [ ] **Multi-zone display** — split-screen layout with independent playlists per zone
  (major architecture change to the display renderer)
- [ ] **HDMI CEC control** — turn TV on/off via `cec-ctl` (Raspberry Pi)
- [ ] **RSS / Atom feed** content source — auto-refresh headlines
- [ ] **Clock / date overlay** — configurable position + style
- [ ] **QR code overlay** — configurable URL
- [ ] **Proof-of-play log** — record item ID, start time, duration played
- [ ] **Prometheus metrics** endpoint (`/metrics`)
- [ ] **Content editor** — basic text-on-colour slide compositor in the admin UI
- [ ] **Android companion app** — WebView wrapper pointing at `http://device-ip:8080`
