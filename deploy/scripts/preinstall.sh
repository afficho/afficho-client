#!/bin/sh
set -e

# Create system user and required directories for Afficho.
if ! id afficho >/dev/null 2>&1; then
    useradd --system --no-create-home --shell /usr/sbin/nologin afficho
fi

mkdir -p /var/lib/afficho /var/log/afficho
chown afficho:afficho /var/lib/afficho /var/log/afficho
