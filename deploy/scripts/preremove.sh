#!/bin/sh
set -e

systemctl stop afficho.service || true
systemctl disable afficho.service || true
