#!/usr/bin/env sh
set -eu

UNIT_SOURCE="${UNIT_SOURCE:-deploy/systemd/p2pbk.service}"
UNIT_TARGET="${UNIT_TARGET:-/etc/systemd/system/p2pbk.service}"

install -m 0644 "$UNIT_SOURCE" "$UNIT_TARGET"
systemctl daemon-reload
systemctl enable p2pbk.service
echo "installed p2pbk.service"
