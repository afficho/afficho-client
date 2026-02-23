# Afficho Client — Implementation TODOs

Open work is tracked as [GitHub Issues](https://github.com/afficho/afficho-client/issues).
This file serves as a high-level roadmap and historical record of completed phases.

---

## Edition model

| Feature | CE (this repo) | EE (Afficho Cloud) |
|---|---|---|
| Admin auth | Single password via config | SSO + RBAC |
| Device management | Local only | Web console |
| Display control | WebSocket (local) | Cloud-pushed via WebSocket |
| Scheduling | Local cron | Cloud-managed |

---

## Completed phases

### Phase 1 — Core Infrastructure ✓
Project layout, TOML config, SQLite schema, chi HTTP server, structured logging,
signal handling, multi-arch builds, dev container, CI/CD, Makefile, migration
versioning, SIGHUP reload, embedded assets, linter config, Apache 2.0 license.

### Phase 2 — Admin Authentication (CE) ✓
`requireAuth()` middleware (HTTP Basic Auth + session cookie), applied to `/admin`
and `/api/v1` write routes. Display endpoints stay unauthenticated for local Chromium.

### Phase 3 — WebSocket Display Control ✓
`/ws/display` endpoint with Hub fan-out. Message types: `current`, `reload`, `alert`,
`clear_alert`. Client-side reconnect with exponential backoff + polling fallback.

### Phase 4 — Content Management: Web Pages ✓
Full CRUD for URL content items with validation, iframe sandboxing, per-item
`allow_popups` flag.

### Phase 5 — Content Management: Images & Video ✓ (1 remaining)
Local media storage with download + multipart upload, magic byte validation,
size tracking. Inline HTML content type.

- Cache eviction: #1

### Phase 6 — Playlist Management API ✓
Full CRUD, ordered items with duration overrides, activate/deactivate,
auto-created default playlist.

### Phase 7 — Scheduler ✓
Queue with auto-advance, periodic reload, cron-based time windows with priority,
graceful empty-playlist handling.

### Phase 8 — Admin UI ✓
Server-rendered templates + HTMX. Dashboard, content library, playlist editor,
storage stats, live preview via WebSocket. Responsive layout.

### Phase 8.5 — Duration belongs to playlists ✓
Playlist-level duration as primary, content-level as fallback. API accepts both
`duration_s` and legacy `duration_override_s`.

### Phase 8.6 — Display improvements ✓
Double-buffer rendering, same-item skip, timeout fallback. Configurable progress bar.

### Phase 9 — Hardware & OS Integration ✓
systemd service, install script, screen power schedule, system info endpoint,
health check, Wayland support, browser auto-detection.

### Phase 10 — Security ✓
CORS, rate limiting, path traversal defense, CSP, security headers.

### Phase 11 — Testing ✓
Unit tests for config, scheduler, content manager, auth, CORS, media safety,
WebSocket Hub. Integration tests for REST API. Benchmarks.

### Phase 12 — Packaging & Distribution ✓ (1 remaining)
goreleaser, `.deb` packages, Docker image + Compose, auto-updater.

- Pi OS image recipe (pi-gen): #2

---

## Open work — tracked as GitHub Issues

### Phase 13 — Google Cast Integration

- #3 — Discovery & configuration (13A)
- #4 — Direct media casting via Default Media Receiver (13B)
- #5 — Custom Web Receiver for URL/HTML content (13C)
- #6 — Multi-device casting (13D)

### Phase 14 — RevealJS Presentation Slides

- #7 — RevealJS slides (backend, display, editor, HTML upload)

### Phase 15 — Document Slides

- #8 — Google Slides embed URL detection (15A)
- #9 — PowerPoint & LibreOffice Impress conversion (15B)
- #10 — PDF slide conversion (15C)

### Backlog

- #11 — Emergency alert overlay (cloud-pushed)
- #12 — Ticket / queue display
- #13 — Multi-zone display
- #14 — HDMI CEC control
- #15 — RSS / Atom feed content source
- #16 — Clock / date overlay
- #17 — QR code overlay
- #18 — Prometheus metrics endpoint
- #19 — Content editor (text-on-colour compositor)
- #20 — Android companion app

---

## Cloud Sync (Afficho Cloud / EE) ✓

All cloud sync tasks are complete (CS-1 through CS-12).

Cloud backend lives in `afficho-cloud` repo. Shared wire-format types in `afficho-types`.

| Task | Description | Status |
|------|-------------|--------|
| CS-1 | Shared types migration | ✓ |
| CS-2 | Config & device identity | ✓ |
| CS-3 | Cloud connector (WebSocket + reconnect) | ✓ |
| CS-4 | Heartbeat | ✓ |
| CS-5 | Content sync handler | ✓ |
| CS-6 | Playlist sync handler | ✓ |
| CS-7 | Schedule sync handler | ✓ |
| CS-8 | Command handler (reload/reboot/update/screenshot) | ✓ |
| CS-9 | Cloud alert handling | ✓ |
| CS-10 | Proof-of-play logger | ✓ |
| CS-11 | Offline resilience + cloud status API | ✓ |
| CS-12 | DB schema changes (migrations 4–7) | ✓ |

**Note:** `ProofOfPlayRecord` is defined locally in `internal/cloud/playlog.go`.
Promote to `afficho-types` when the cloud service is ready to receive it.
