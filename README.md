# afficho-client

The open-source **Afficho** digital signage client daemon. Runs on Raspberry Pi, Android TV sticks, tablets, and any Linux system. Displays content (images, videos, web pages) in a browser kiosk and provides a local HTTP API and admin UI for management.

## Architecture

```
┌────────────────────────────── Device ──────────────────────────────┐
│                                                                     │
│   ┌─────────────────────┐        ┌──────────────────────────────┐  │
│   │     Go Daemon        │◄──────►│   Chromium (kiosk mode)      │  │
│   │                     │  HTTP  │                              │  │
│   │  · Scheduler        │        │  Renders content from        │  │
│   │  · Content cache    │        │  localhost:8080/display       │  │
│   │  · REST API         │        │                              │  │
│   │  · Admin UI         │        │  Images · Video · Web pages  │  │
│   │  · Cloud sync       │        │                              │  │
│   └─────────────────────┘        └──────────────────────────────┘  │
│             │                                                       │
│             ▼                                                       │
│   SQLite + local media files (/var/lib/afficho)                    │
└────────────────────────────────────────────────────────────────────┘
```

The daemon owns the scheduling and content logic. Chromium is just a dumb renderer — it polls `/display/current` and renders whatever the daemon says is active.

## Supported Platforms

| Platform | Architecture | Notes |
|---|---|---|
| Raspberry Pi 4/5 | `linux/arm64` | Primary target |
| Raspberry Pi 2/3 | `linux/armv7` | Supported |
| Raspberry Pi Zero 2 | `linux/armv6` | Supported |
| Android TV sticks/tablets | `linux/arm64` via Termux | Experimental |
| x86-64 (dev/testing) | `linux/amd64` | Full support |

## Quick Start

### Requirements

- Go 1.24+ (to build from source)
- Chromium or Google Chrome

### Install from release

```bash
# Replace with your architecture: linux-arm64, linux-armv7, linux-amd64
curl -L https://github.com/afficho/afficho-client/releases/latest/download/afficho-client_v0.1.0_linux-arm64.tar.gz \
  | tar -xz
sudo mv afficho-client_* /usr/local/bin/afficho
```

### Build from source

```bash
git clone https://github.com/afficho/afficho-client
cd afficho-client
# Open in VS Code → "Reopen in Container", then in the container terminal:
make build
```

### Run

```bash
# Copy and edit the example config
cp config.example.toml config.toml

# Start the daemon
./bin/afficho -config config.toml
```

Open **http://localhost:8080/admin** to manage content.
Open **http://localhost:8080/display** to preview what the screen shows.

## Configuration

All settings live in a TOML config file. See [`config.example.toml`](./config.example.toml) for all options.

| Setting | Default | Description |
|---|---|---|
| `server.port` | `8080` | HTTP listen port |
| `admin.password` | `""` (empty) | Admin password (empty = auth disabled) |
| `display.launch_browser` | `true` | Auto-launch Chromium on startup |
| `display.browser` | `chromium-browser` | Browser executable name |
| `storage.data_dir` | `/var/lib/afficho` | Database and media storage path |
| `storage.max_cache_gb` | `10` | Maximum media cache size |
| `cloud.enabled` | `false` | Enable Afficho Cloud sync |
| `logging.debug` | `false` | Verbose logging |

## Authentication

The Community Edition uses a single admin password set in the config file.

```toml
[admin]
password = "your-secret-password"
```

When a password is set:

- `/admin`, `/admin/*`, and all `/api/v1` write endpoints require authentication
- The browser prompts for credentials via HTTP Basic Auth
- After login a signed session cookie (`afficho_session`, 24h) avoids re-prompting
- Changing the password in the config invalidates all existing sessions

These routes remain **unauthenticated** (Chromium on the device needs them):

- `/display`, `/display/current` — display renderer
- `/media/*` — static media files
- `GET /api/v1/status` — read-only status (useful for monitoring)

When the password is empty (the default), authentication is disabled entirely.

> **Security note:** The CE password protects against casual access on a local
> network. Do not expose port 8080 to the internet without a reverse proxy
> (nginx, Caddy) providing TLS. Enterprise authentication (SSO, RBAC) is
> handled by the Afficho Cloud web console, not by this daemon.

## API

The daemon exposes a REST API at `/api/v1`. All responses are JSON.

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/status` | Daemon status + current item |
| `GET` | `/api/v1/content` | List content items |
| `POST` | `/api/v1/content` | Add content (URL or upload) |
| `DELETE` | `/api/v1/content/{id}` | Remove content item |
| `GET` | `/api/v1/playlists` | List playlists |
| `POST` | `/api/v1/playlists` | Create playlist |
| `PUT` | `/api/v1/playlists/{id}/items` | Set playlist items (ordered) |
| `POST` | `/api/v1/playlists/{id}/activate` | Set as active playlist |
| `GET` | `/api/v1/scheduler/status` | Queue + current item |
| `POST` | `/api/v1/scheduler/next` | Force advance to next item |

> Note: Most endpoints are not yet implemented — see [TODOS.md](./TODOS.md).

## Content Types

| Type | Description |
|---|---|
| `image` | JPEG, PNG, GIF, WebP — served from local storage |
| `video` | MP4, WebM — served from local storage |
| `url` | External web page rendered in an iframe |
| `html` | Inline HTML snippet |

### iframe sandboxing (URL / HTML content)

Web pages are rendered inside an `<iframe>` with the sandbox policy
`allow-scripts allow-same-origin allow-forms`. This prevents embedded content
from navigating the top-level page or accessing browser APIs it shouldn't.

Per-item opt-in: set `"allow_popups": true` on a content item to also grant the
`allow-popups` sandbox token (needed for sites that open links in new tabs).

**Known limitations:** Some external sites block embedding via the
`X-Frame-Options` header or `Content-Security-Policy: frame-ancestors` directive.
These restrictions are enforced by the browser and cannot be bypassed client-side.
If a URL shows a blank iframe, check the site's response headers.

## Development

### Dev Container (recommended)

Open the project in VS Code and select **"Reopen in Container"**. The container includes Go 1.24, golangci-lint, and GitHub CLI. `go mod tidy` runs automatically on first open to generate `go.sum`.

Port `8080` is forwarded automatically — open it in your browser after `make dev`.

All `go` and `make` commands below should be run **inside the dev container terminal**.

```bash
# Run with example config (no browser launched, opens on port 8080)
make dev

# Admin UI: http://localhost:8080/admin
# Display:  http://localhost:8080/display
# Status:   http://localhost:8080/api/v1/status
```

### Available make targets

```
make build        Build for current platform
make build-all    Build for all platforms (amd64, arm64, armv7, armv6)
make test         Run tests
make lint         Run golangci-lint
make dev          Run with example config
make clean        Remove build artifacts
```

## Running on Raspberry Pi

1. Install Raspberry Pi OS Lite (64-bit recommended for Pi 4/5)
2. Install Chromium: `sudo apt install chromium-browser`
3. Install and configure the daemon:

```bash
sudo cp afficho /usr/local/bin/
sudo mkdir -p /etc/afficho
sudo cp config.example.toml /etc/afficho/config.toml
# Edit /etc/afficho/config.toml: set launch_browser = true
```

4. Install the systemd service:

```bash
# Coming soon — see TODOS.md
```

## License

Apache 2.0 — see [LICENSE](./LICENSE).
