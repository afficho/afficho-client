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

## Phase 10 — Security

- [x] Auth design decided: CE = single config password, EE = cloud console SSO/RBAC
- [x] `requireAuth()` middleware (completed in Phase 2)
- [x] CORS: restrict `/api/v1` to same origin by default; configurable allowlist
- [x] Rate limiting on upload endpoint (prevent disk fill)
- [x] Path traversal audit on `/media/` file server
- [x] iframe sandbox policy review per content type (Phase 4–5)
- [x] Optional TLS: document reverse-proxy setup (nginx / Caddy) for HTTPS
- [x] Security note in README: do not expose port 8080 to the internet unprotected
- [x] Security response headers (`X-Content-Type-Options`, `X-Frame-Options`, `Referrer-Policy`)
- [x] CSP header for inline HTML content rendering

---

## Phase 11 — Testing

- [x] Unit: config loading (defaults, file override, missing file)
- [x] Unit: scheduler queue logic (advance, wrap-around, empty queue, reload)
- [x] Unit: scheduler TimeWindow parsing and IsActive evaluation
- [x] Unit: content manager path traversal guard
- [x] Unit: content manager media type validation (magic bytes)
- [x] Unit: `requireAuth` middleware (with / without password, valid / invalid token)
- [x] Unit: CORS middleware (same-origin, allowlist, wildcard, preflight)
- [x] Unit: safe media middleware (null bytes, dotfiles)
- [x] Unit: WebSocket Hub (register, unregister, broadcast, slow-client drop)
- [x] Integration: REST API via `net/http/httptest`
  - Content CRUD, playlist CRUD, status, healthz, storage, display, auth enforcement
- [x] Integration: DB migrations (tables created, default playlist seeded, schema version)
- [x] Benchmark: scheduler `Current()` under concurrent read load

---

## Phase 12 — Packaging & Distribution

- [x] Use goreleaser for semantic versioning (`.goreleaser.yaml` — builds, archives, changelog)
- [x] `.deb` package (using `nfpm`) for Raspberry Pi OS / Debian (nfpm via goreleaser)
- [x] Docker image (`FROM debian:bookworm-slim`) for dev/testing (`Dockerfile`)
- [x] Docker Compose file: daemon + Chromium in headless mode (`docker-compose.yml`)
- [ ] Raspberry Pi OS image recipe (pi-gen) for zero-setup SD card flashing
  — **Deferred**: `.deb` package covers 90% of use cases; pi-gen adds value when
  Afficho Cloud (EE) exists for fleet provisioning
- [x] Auto-update: check GitHub releases API, download, verify checksum, stage binary
  (`internal/updater/`), API endpoints (`GET /api/v1/update/status`,
  `POST /api/v1/update/check`)

---

## Phase 13 — Google Cast Integration

Cast content to Chromecast / Google Cast devices on the local network.
The daemon acts as a Cast **sender** — it discovers Cast devices via mDNS,
connects to them, and pushes content using the same scheduler that drives
the local Chromium display.

### 13A — Discovery & configuration

- [ ] Add `[cast]` config section:
  ```toml
  [cast]
  enabled = false
  # Filter by device name (empty = first found, "*" = all discovered)
  device_name = ""
  # How often to scan for new Cast devices on the network (seconds)
  discovery_interval = 60
  ```
- [ ] Add `CastConfig` to `internal/config/config.go`
- [ ] mDNS / SSDP discovery: find Cast devices on the LAN
  - Use `github.com/vishen/go-chromecast` or `github.com/barnybug/go-cast`
  - Evaluate both libraries; prefer the one with fewer dependencies and
    active maintenance
- [ ] `internal/cast/discovery.go` — background goroutine that periodically
  scans the network, maintains a list of discovered devices
- [ ] `GET /api/v1/cast/devices` — list discovered Cast devices
  (name, model, IP, port, status)
- [ ] Admin UI: Cast devices section — show discovered devices, allow
  selecting which device to cast to

### 13B — Direct media casting (Default Media Receiver)

Cast images and videos directly to Chromecast using the built-in Default
Media Receiver (app ID `CC1AD845`). No Google registration needed.
This covers the most common digital signage use case: image/video playlists.

- [ ] `internal/cast/sender.go` — Cast sender that manages a connection to
  one or more Cast devices
- [ ] Cast `image` content: load image URL via Default Media Receiver
  - The Cast device fetches `http://<afficho-ip>:8080/media/{file}` directly
  - Requires the Afficho daemon to be reachable from the Cast device (same LAN)
- [ ] Cast `video` content: load video URL via Default Media Receiver
  - Same network requirement
  - Handle `video.ended` equivalent: the Cast SDK reports media status,
    advance scheduler when playback finishes
- [ ] Duration management: for images, send a load command with idle timeout
  matching the scheduler's `duration_s`; for videos, advance on media completion
  or `duration_s`, whichever comes first
- [ ] Integrate with scheduler: `cast.Sender` subscribes to `scheduler.OnChange`
  (or a new callback) and pushes the new item to the Cast device on each advance
- [ ] Handle Cast device disconnect/reconnect gracefully:
  - Reconnect automatically when device reappears on the network
  - Resume current content from scheduler state
- [ ] `POST /api/v1/cast/start` — begin casting to a discovered device
- [ ] `POST /api/v1/cast/stop` — stop casting
- [ ] `GET /api/v1/cast/status` — current cast state (device, playing item, connected)

### 13C — URL and HTML content on Cast (Custom Web Receiver)

The Default Media Receiver can't render web pages or HTML. To get full
feature parity (URL iframes, HTML content, RevealJS slides, transitions,
alerts), the Afficho project registers a **Custom Web Receiver** with Google.

- [ ] Build a Custom Web Receiver: minimal HTML page that:
  - Accepts `ws_url` parameter (the daemon's WebSocket endpoint)
  - Connects to the daemon's `/ws/display` WebSocket
  - Renders content identically to the local `/display` page
  - Hosted at `https://cast.afficho.io/receiver.html` (project-maintained)
- [ ] Register the Custom Web Receiver with Google Cast SDK (one-time,
  done by project maintainers — returns an app ID)
- [ ] Update `cast.Sender` to use the Custom Receiver app ID for `url`,
  `html`, and `slides` content types
- [ ] The Cast device loads the receiver page, which connects back to the
  daemon's WebSocket — all existing display features work automatically
- [ ] Fallback: if Custom Receiver is unavailable (offline, Google changes
  policy), log a warning and skip non-media content types

### 13D — Multi-device casting

- [ ] Support casting to multiple Cast devices simultaneously
- [ ] Config: allow a list of device names or "all"
  ```toml
  [cast]
  device_name = ["Kitchen TV", "Lobby Display"]
  ```
- [ ] Each Cast device can show the same content (mirror mode) or be assigned
  to a different playlist (independent mode — future, requires per-device
  playlist assignment)
- [ ] Admin UI: multi-device status overview, start/stop per device

---

## Phase 14 — RevealJS Presentation Slides

New content type `"slides"` powered by RevealJS. Users create slide decks
in the admin UI (or upload RevealJS HTML). The display renders them as
full-screen auto-advancing presentations.

### Backend

- [ ] Embed RevealJS 5.x library in `web/static/revealjs/`
  - `reveal.min.js`, `reveal.min.css`, bundled themes
  - Adds ~200 KB to the binary (gzipped)
- [ ] New content type `type: "slides"` in `content_items`
  - Update the `CHECK` constraint in migration:
    `CHECK(type IN ('image','video','url','html','slides'))`
- [ ] Slide data stored as JSON in the `source` column:
  ```json
  {
    "slides": [
      { "content": "<h1>Welcome</h1><p>To our store</p>" },
      { "content": "<h2>Today's Special</h2><ul><li>50% off</li></ul>" },
      { "content": "## Markdown Slide\n\nRevealJS supports **markdown**" }
    ],
    "theme": "black",
    "transition": "slide",
    "auto_slide_ms": 0
  }
  ```
  - `auto_slide_ms: 0` means "derive from content item duration_s / slide count"
  - Non-zero value overrides: each slide displays for exactly that many ms
- [ ] `POST /api/v1/content` with `type: "slides"`:
  ```json
  {
    "name": "Welcome Slides",
    "type": "slides",
    "slides": [...],
    "theme": "black",
    "transition": "slide"
  }
  ```
  - Validate: at least 1 slide, each slide has non-empty content
  - Store serialized JSON in `source`
  - `duration_s` defaults to `10 * slide_count` if not provided
- [ ] `PATCH /api/v1/content/{id}` — update slides, theme, transition
  - Accept `slides` field for partial update (replace all slides)
- [ ] Extend `/content/{id}/render` to handle `type: "slides"`:
  - Parse JSON from `source`
  - Generate full RevealJS HTML page:
    - Include embedded `reveal.min.js` and theme CSS
    - Render each slide as a `<section>`
    - Configure `autoSlide` based on item duration or `auto_slide_ms`
    - Enable `loop: false` (play through once, then signal completion)
  - Set appropriate CSP headers for RevealJS execution
- [ ] Signal presentation completion to the scheduler:
  - Option A: RevealJS `slidechanged` event — when the last slide has been
    shown for its duration, call `POST /display/advance` (same pattern as
    video `ended` event)
  - Option B: rely on the scheduler's `duration_s` timer (simpler, works
    even if JS fails, but less precise)
  - **Use both:** RevealJS signals completion via `/display/advance`, and
    the scheduler timer acts as a fallback
- [ ] RevealJS themes available: `black`, `white`, `league`, `beige`, `sky`,
  `night`, `serif`, `simple`, `solarized`, `moon`, `dracula`
  - Map to RevealJS built-in theme CSS files
- [ ] RevealJS transitions: `none`, `fade`, `slide`, `convex`, `concave`, `zoom`

### Display page integration

- [ ] Update `createContentElement()` in `display.html` to handle `type: "slides"`:
  - Create an `<iframe>` pointing to `/content/{id}/render`
  - Same sandbox as `html` type: `allow-scripts`
  - Pass duration info so RevealJS can calculate per-slide timing
- [ ] The iframe loads the RevealJS presentation, which auto-advances and
  signals completion — no changes needed to the double-buffer or swap logic

### Admin UI — Slide editor

- [ ] "Add Slides" button in the content library (alongside URL, Upload, HTML)
- [ ] Slide editor page:
  - Left panel: slide list (thumbnails/numbers), drag-to-reorder, add/delete
  - Right panel: slide content editor
    - Toggle between HTML and Markdown editing modes
    - Live preview of the current slide (rendered in a small iframe)
  - Theme selector dropdown
  - Transition selector dropdown
  - Auto-advance timing: "Auto (from duration)" or custom ms per slide
- [ ] Slide content supports:
  - HTML (direct `<h1>`, `<p>`, `<img>`, etc.)
  - Markdown (RevealJS has built-in Markdown support via the `marked` plugin)
  - Inline images (base64 data URLs or `/media/` references)

### RevealJS HTML upload

- [ ] Alternative creation path: upload a complete RevealJS HTML file
  - `POST /api/v1/content` with `type: "slides"` and multipart upload
  - Validate: must contain RevealJS initialization (`Reveal.initialize`)
  - Store the raw HTML in `source` as `{ "raw_html": "..." }`
  - Render endpoint serves it directly (with Afficho's RevealJS assets)
- [ ] Use case: designers build slides in RevealJS externally, upload the
  finished file to Afficho for display

---

## Phase 15 — Document Slides (Google Slides, PPTX, ODP)

Display slide decks from Google Slides, Microsoft PowerPoint, and
LibreOffice Impress.

### 15A — Google Slides (embed URL)

Google Slides already works as a `type: "url"` content item by pasting the
published embed URL. This phase adds smart detection and quality-of-life
improvements.

- [ ] URL detection: when creating content with `type: "url"`, detect Google
  Slides URLs and offer to enhance them:
  - Detect pattern: `docs.google.com/presentation/d/{id}/...`
  - Auto-rewrite to embed URL:
    `https://docs.google.com/presentation/d/{id}/embed?start=true&loop=true&delayms={ms}`
  - Calculate `delayms` from `duration_s / estimated_slide_count` or use
    a sensible default (5000 ms)
- [ ] `POST /api/v1/content` — new optional field `google_slides_id`:
  - If provided, auto-generate the embed URL
  - Store original presentation ID in metadata for future reference
- [ ] Admin UI: "Add Google Slides" shortcut in content library
  - Input: paste any Google Slides URL (view, edit, or embed)
  - Auto-extract the presentation ID
  - Configure: slide advance delay (seconds), start from beginning, loop
  - Preview before adding
- [ ] Document in the admin UI: the Google Slides presentation must be
  published to the web (`File → Share → Publish to web`) or at least set
  to "Anyone with the link can view"
- [ ] Handle Google Slides URL variants:
  - `/edit` URLs → convert to `/embed`
  - `/pub` URLs → convert to `/embed`
  - Already `/embed` → use as-is, ensure query params are set
- [ ] Limitations to document:
  - Requires internet access from the display device
  - Google controls rendering — no offline support
  - Some animations/transitions may not work in embed mode

### 15B — PowerPoint & LibreOffice Impress (file conversion)

Upload `.pptx`, `.ppt`, `.odp`, or `.key` files. Convert to images using
LibreOffice headless mode. Store each slide as an image and create a
slides content item that cycles through them.

**Dependency:** LibreOffice must be installed on the device. This is an
optional feature — if LibreOffice is not available, the upload is rejected
with a helpful error message explaining how to install it.

#### Conversion pipeline

- [ ] `internal/slides/converter.go` — slide file → image conversion:
  - Accept file path, output directory
  - Run: `libreoffice --headless --convert-to png --outdir {dir} {file}`
  - Parse output to determine how many slides were generated
  - Return list of generated image paths
  - Timeout: 60s per conversion (configurable), kill process on timeout
- [ ] Detect LibreOffice availability on startup:
  - Try `libreoffice --version` (or `soffice --version`)
  - Store result in a capability flag
  - `GET /api/v1/system` — include `"slides_conversion": true/false`
- [ ] Accepted MIME types / extensions:
  - `.pptx` — `application/vnd.openxmlformats-officedocument.presentationml.presentation`
  - `.ppt` — `application/vnd.ms-powerpoint`
  - `.odp` — `application/vnd.oasis.opendocument.presentation`
  - `.key` — `application/x-iwork-keynote-sffkey` (best-effort, LibreOffice
    support for Keynote is limited)
- [ ] Validate magic bytes for each format (same pattern as image/video validation)

#### Content creation flow

- [ ] `POST /api/v1/content` with `type: "slides"` and multipart upload of
  a slide file:
  1. Save uploaded file to temp directory
  2. Run LibreOffice conversion → generates `slide1.png`, `slide2.png`, ...
  3. Move generated images to `data/media/{content_id}/slide-{n}.png`
  4. Create content item with `type: "slides"` and `source` containing:
     ```json
     {
       "format": "converted",
       "slide_count": 12,
       "slide_paths": ["/media/{id}/slide-1.png", "/media/{id}/slide-2.png", ...],
       "original_filename": "quarterly-review.pptx"
     }
     ```
  5. `duration_s` defaults to `10 * slide_count` if not provided
  6. Delete temp file after conversion
- [ ] Handle conversion errors gracefully:
  - LibreOffice not installed → 400 with message:
    `"slide conversion requires LibreOffice (apt install libreoffice-impress)"`
  - Conversion fails → 400 with LibreOffice error output
  - Corrupt/invalid file → 400 with descriptive message
- [ ] Track total size of generated images in `size_bytes`

#### Display rendering

- [ ] Extend `/content/{id}/render` for converted slide decks:
  - Generate an HTML page that cycles through the slide images
  - Use CSS transitions (fade or slide) between images
  - Auto-advance timing: `duration_s / slide_count` per slide
  - Signal completion via `POST /display/advance` after the last slide
- [ ] Alternative: generate a RevealJS presentation with `<img>` slides
  (reuse Phase 14 infrastructure) — this gives RevealJS transitions and
  themes for free
- [ ] Update `createContentElement()` in `display.html`:
  - Converted slide decks render as an iframe to `/content/{id}/render`
  - Same behavior as RevealJS slides

#### Admin UI

- [ ] "Upload Slides" button in content library:
  - Drag-and-drop or file picker for `.pptx`, `.ppt`, `.odp`
  - Show conversion progress (spinner — conversion can take 5-30s)
  - Preview generated slides after conversion
  - Allow re-ordering or removing individual slides before saving
- [ ] Slide deck detail view:
  - Thumbnail grid of all slides
  - Per-slide duration override (default: auto from total duration)
  - Option to re-upload / re-convert (preserves content ID, replaces slides)
- [ ] Show a warning if LibreOffice is not detected:
  "Slide file upload requires LibreOffice. Install with:
  `sudo apt install libreoffice-impress`"

#### Re-conversion on update

- [ ] `PATCH /api/v1/content/{id}` with new file upload:
  - Re-run conversion pipeline
  - Replace old slide images
  - Update slide count and size
  - Trigger scheduler reload + WebSocket broadcast
- [ ] Delete old slide images when content item is deleted

### 15C — PDF slides

PDFs are a common output format for presentations. Support them as a
lightweight alternative to PPTX conversion.

- [ ] Accept `.pdf` uploads for `type: "slides"`
- [ ] Convert PDF pages to images:
  - Option A: `pdftoppm` (from `poppler-utils`, lighter than LibreOffice)
  - Option B: LibreOffice headless (`--convert-to png`)
  - Prefer `pdftoppm` — smaller dependency, faster, better quality
- [ ] Same storage and rendering pipeline as converted PPTX slides
- [ ] Detect `pdftoppm` availability:
  `GET /api/v1/system` — include `"pdf_conversion": true/false`
- [ ] Admin UI: accept `.pdf` in the same upload dialog as PPTX/ODP

---

## Backlog / Nice to Have

- [ ] **Cache eviction** when `storage.max_cache_gb` is exceeded (LRU — delete items
  not in any active playlist first, then oldest by last-played)
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

---

## Cloud Sync (Afficho Cloud / EE)

Cloud backend exists in `afficho-cloud` repo. These items enable the client to
connect to the cloud for fleet management, content distribution, and remote control.
Shared wire-format types are in `afficho-types` repo.

### CS-1 — Shared types migration

- [x] Import `github.com/afficho/afficho-types` in `go.mod`
- [x] Replace `api.Message` (`internal/api/hub.go`) with `types.WSMessage`
  - `api.Message.Payload` is `any`; `types.WSMessage.Payload` is `json.RawMessage`
  - Update `writeMsg()`, `BroadcastCurrent()`, and `handleDisplayWS()` accordingly
  - Update hub's `Broadcast()` to accept `types.WSMessage`
- [x] Use `types.Type*` constants instead of bare strings (`"current"`, `"reload"`, etc.)

### CS-2 — Config & device identity

- [x] Add `[cloud]` config section:
  ```toml
  [cloud]
  enabled = false
  endpoint = "wss://cloud.afficho.io/ws/device"
  device_key = ""           # device key from cloud console registration
  heartbeat_interval = 30   # seconds
  reconnect_max_delay = 300 # max backoff in seconds
  ```
- [x] Add `CloudConfig` to `internal/config/config.go`
- [x] Generate stable device ID on first run (UUIDv4, stored in `device_meta` table)
- [x] Read device ID from `device_meta` on subsequent runs

### CS-3 — Cloud connector

`internal/cloud/connector.go` — persistent WebSocket to cloud.

- [x] Connect to cloud endpoint with `Authorization: Bearer <device_key>` header
- [x] Reconnect with exponential backoff (1s → 2s → 4s → ... → `reconnect_max_delay`)
- [x] On connect: send `types.TypeRegister` message with `types.DeviceRegistration` payload
  (device ID, hostname, arch, OS version, app version, local IP)
- [x] Message dispatch: route incoming `types.WSMessage` by type to handlers
- [x] On disconnect: log warning, start reconnect loop
- [x] Graceful shutdown: close WebSocket on SIGINT/SIGTERM

### CS-4 — Heartbeat

- [x] Send `types.TypeHeartbeat` message with `types.Heartbeat` payload every
  `cloud.heartbeat_interval` seconds over the existing WebSocket
- [x] Include: device ID, current item ID, playlist ID, uptime, CPU temp,
  memory/disk usage, storage used, screen state, timestamp
- [x] Handle `types.TypeHeartbeatAck` response (may carry pending commands)

### CS-5 — Content sync handler

`internal/cloud/content.go` — receive and cache content from cloud.

- [x] Handle `types.TypeSyncContent` message: receive list of `types.ContentSyncItem`
- [x] Compare checksums with local media cache:
  - Already cached + checksum matches → skip
  - New or changed → download from signed URL, verify SHA-256 checksum
  - Present locally but absent from manifest → delete (cloud removed it)
- [x] Download with timeout + context propagation (addresses existing TODO in
  `internal/content/manager.go:65`)
- [x] Send `types.TypeSyncAck` with `types.SyncAck{SyncType: "content"}` on completion
- [x] Store cloud-synced content in the existing `content_items` table
  with an `origin = 'cloud'` column to distinguish from locally-created content

### CS-6 — Playlist sync handler

`internal/cloud/playlist.go` — receive playlists from cloud.

- [x] Handle `types.TypeSyncPlaylist` message: receive `[]types.PlaylistSync` payload
- [x] Upsert playlist + items in local SQLite (transactional replace)
- [x] Flag cloud-pushed playlists as `origin = 'cloud'` in the DB
  (prevents local admin UI from editing cloud-managed playlists)
- [x] Cloud always wins: on conflict, cloud state replaces local state
  for `origin = 'cloud'` playlists
- [x] Device retains local playlists (`origin = 'local'`) for offline operation
- [x] Trigger `scheduler.TriggerReload()` after sync
- [x] Send `types.TypeSyncAck` with `types.SyncAck{SyncType: "playlist"}` on completion

### CS-7 — Schedule sync handler

`internal/cloud/schedule.go` — receive schedules from cloud.

- [x] Handle `types.TypeSyncSchedule` message: receive `[]types.ScheduleSync` payload
- [x] Upsert schedule definitions in local SQLite
- [x] Flag as `origin = 'cloud'` (same pattern as playlists)
- [x] Validate cron expressions with `scheduler.ParseTimeWindow()` before storing
- [x] Trigger `scheduler.TriggerReload()` after sync
- [x] Send `types.TypeSyncAck` with `types.SyncAck{SyncType: "schedule"}` on completion

### CS-8 — Command handler

- [x] Handle `types.TypeCommand` message: receive `types.DeviceCommand` payload
- [x] Dispatch commands:
  - `reload` → broadcast `types.TypeReload` to display WebSocket
  - `reboot` → execute system reboot (3s delay, systemctl fallback)
  - `update` → trigger self-update via `internal/updater/`
  - `screenshot` → capture screen (scrot/import/xwd), send base64
    `types.ScreenshotResponse` back as `types.TypeScreenshot` message

### CS-9 — Cloud alert handling

- [x] Handle `types.TypeAlert` message: receive `types.AlertMessage` payload
  → broadcast to local display WebSocket (reuses existing alert rendering)
- [x] Handle `types.TypeClearAlert` message → broadcast clear to local display

### CS-10 — Proof of play logger

`internal/cloud/playlog.go` — record and report content playback.

- [x] Record content item transitions: content ID, start time, actual duration played
- [x] Store records in a local SQLite table (`proof_of_play`)
- [x] Batch and send to cloud periodically as `types.TypeProofOfPlay` message
  (uses local `ProofOfPlayRecord` type — promote to `afficho-types` when ready)
- [x] If offline: accumulate locally, retry on next flush cycle
- [x] Configurable batch size (default 50) and send interval (default 60s)

### CS-11 — Offline resilience

- [ ] Operate fully from local DB when cloud is unreachable
- [ ] On reconnect: cloud re-evaluates and pushes resolved state
  (playlists, schedules, content manifest)
- [ ] Flush pending proof-of-play records on reconnect
- [ ] `GET /api/v1/cloud/status` — device ID, cloud connection state,
  last sync timestamp, pending proof-of-play count

### CS-12 — DB schema changes

- [ ] Add `source` column (`TEXT DEFAULT 'local'`) to `playlists` table
  (values: `'local'`, `'cloud'`)
- [ ] Add `source` column (`TEXT DEFAULT 'local'`) to `content_items` table
- [ ] Add `source` column (`TEXT DEFAULT 'local'`) to `schedules` table
- [ ] Create `proof_of_play` table:
  ```sql
  CREATE TABLE proof_of_play (
      id TEXT PRIMARY KEY,
      content_id TEXT NOT NULL,
      started_at TEXT NOT NULL,
      duration_s INTEGER NOT NULL,
      synced BOOLEAN DEFAULT FALSE
  );
  ```
- [ ] Migration to add new columns and table
