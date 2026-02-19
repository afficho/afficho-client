#!/usr/bin/env bash
#
# Afficho Client — Install Script
#
# Installs the afficho binary, creates a systemd service, and sets up the
# required directories and configuration. Must be run as root.
#
# Usage:
#   sudo bash scripts/install.sh [--bin-dir DIR]
#
# Options:
#   --bin-dir DIR    Directory containing pre-built binaries (default: ./bin)
#   --help           Show this help message
#
set -euo pipefail

BIN_DIR="./bin"
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/afficho"
DATA_DIR="/var/lib/afficho"
LOG_DIR="/var/log/afficho"
SERVICE_USER="afficho"
BINARY_NAME="afficho"

usage() {
    sed -n '3,/^$/s/^# \?//p' "$0"
    exit 0
}

info()  { printf '\033[1;34m[INFO]\033[0m  %s\n' "$*"; }
warn()  { printf '\033[1;33m[WARN]\033[0m  %s\n' "$*"; }
error() { printf '\033[1;31m[ERROR]\033[0m %s\n' "$*" >&2; exit 1; }

# ── Parse arguments ───────────────────────────────────────────────────────────

while [[ $# -gt 0 ]]; do
    case "$1" in
        --bin-dir) BIN_DIR="$2"; shift 2 ;;
        --help|-h) usage ;;
        *) error "Unknown option: $1" ;;
    esac
done

# ── Pre-flight checks ────────────────────────────────────────────────────────

[[ $EUID -eq 0 ]] || error "This script must be run as root (use sudo)"

# ── Detect architecture ──────────────────────────────────────────────────────

detect_arch() {
    local arch
    arch="$(uname -m)"
    case "$arch" in
        x86_64)                  echo "amd64" ;;
        aarch64|arm64)           echo "arm64" ;;
        armv7*|armhf)            echo "armv7" ;;
        armv6*|arm)              echo "armv6" ;;
        *) error "Unsupported architecture: $arch" ;;
    esac
}

ARCH="$(detect_arch)"
BINARY_SRC="${BIN_DIR}/${BINARY_NAME}-linux-${ARCH}"

if [[ ! -f "$BINARY_SRC" ]]; then
    # Try the plain binary name (single-platform build).
    if [[ -f "${BIN_DIR}/${BINARY_NAME}" ]]; then
        BINARY_SRC="${BIN_DIR}/${BINARY_NAME}"
    else
        error "Binary not found: $BINARY_SRC\nRun 'make build-all' first."
    fi
fi

info "Detected architecture: $ARCH"
info "Using binary: $BINARY_SRC"

# ── Create system user ───────────────────────────────────────────────────────

if ! id "$SERVICE_USER" &>/dev/null; then
    info "Creating system user: $SERVICE_USER"
    useradd --system --no-create-home --shell /usr/sbin/nologin "$SERVICE_USER"
else
    info "User $SERVICE_USER already exists"
fi

# ── Install binary ───────────────────────────────────────────────────────────

info "Installing binary to $INSTALL_DIR/$BINARY_NAME"
install -m 0755 "$BINARY_SRC" "$INSTALL_DIR/$BINARY_NAME"

# ── Create directories ───────────────────────────────────────────────────────

info "Creating directories"
mkdir -p "$CONFIG_DIR" "$DATA_DIR" "$LOG_DIR"
chown "$SERVICE_USER:$SERVICE_USER" "$DATA_DIR" "$LOG_DIR"

# ── Install config ───────────────────────────────────────────────────────────

if [[ ! -f "$CONFIG_DIR/config.toml" ]]; then
    if [[ -f "config.example.toml" ]]; then
        info "Installing default config to $CONFIG_DIR/config.toml"
        install -m 0640 -o root -g "$SERVICE_USER" config.example.toml "$CONFIG_DIR/config.toml"
    else
        warn "config.example.toml not found — skipping config installation"
    fi
else
    info "Config already exists at $CONFIG_DIR/config.toml — not overwriting"
fi

# ── Install systemd service ──────────────────────────────────────────────────

info "Installing systemd service"
install -m 0644 deploy/afficho.service /etc/systemd/system/afficho.service
systemctl daemon-reload

# ── Install logrotate config ─────────────────────────────────────────────────

if [[ -d /etc/logrotate.d ]]; then
    info "Installing logrotate config"
    install -m 0644 deploy/logrotate.d/afficho /etc/logrotate.d/afficho
fi

# ── Enable and start ─────────────────────────────────────────────────────────

info "Enabling and starting afficho service"
systemctl enable afficho
systemctl start afficho

# ── Summary ───────────────────────────────────────────────────────────────────

LOCAL_IP="$(hostname -I 2>/dev/null | awk '{print $1}')"
VERSION="$("$INSTALL_DIR/$BINARY_NAME" -version 2>/dev/null || echo "unknown")"

echo ""
echo "========================================"
echo "  Afficho Client installed successfully"
echo "========================================"
echo ""
echo "  Version:    $VERSION"
echo "  Binary:     $INSTALL_DIR/$BINARY_NAME"
echo "  Config:     $CONFIG_DIR/config.toml"
echo "  Data:       $DATA_DIR"
echo "  Service:    afficho.service"
echo ""
echo "  Admin UI:   http://${LOCAL_IP:-localhost}:8080/admin"
echo "  Display:    http://${LOCAL_IP:-localhost}:8080/display"
echo ""
echo "  Commands:"
echo "    systemctl status afficho    # check status"
echo "    journalctl -u afficho -f    # view logs"
echo "    systemctl restart afficho   # restart"
echo ""
