#!/bin/sh
set -e

systemctl daemon-reload
systemctl enable afficho.service

# Only start on fresh install, not upgrade.
# dpkg passes "configure" with an empty second arg on first install.
if [ "$1" = "configure" ] && [ -z "$2" ]; then
    systemctl start afficho.service
fi
