#!/usr/bin/env sh
set -eu

UNIT_SOURCE="${UNIT_SOURCE:-deploy/systemd/backupd.service}"
UNIT_TARGET="${UNIT_TARGET:-/etc/systemd/system/backupd.service}"

install -m 0644 "$UNIT_SOURCE" "$UNIT_TARGET"
systemctl daemon-reload
systemctl enable backupd.service
echo "installed backupd.service"
