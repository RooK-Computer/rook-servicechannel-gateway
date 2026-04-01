#!/bin/sh
set -eu

if ! getent group rook-gateway >/dev/null 2>&1; then
  addgroup --system rook-gateway >/dev/null
fi

if ! getent passwd rook-gateway >/dev/null 2>&1; then
  adduser \
    --system \
    --ingroup rook-gateway \
    --home /nonexistent \
    --no-create-home \
    --shell /usr/sbin/nologin \
    rook-gateway >/dev/null
fi

if command -v systemctl >/dev/null 2>&1; then
  systemctl daemon-reload >/dev/null 2>&1 || true
fi

echo "rook-servicechannel-gateway installed; the systemd service remains disabled by default."
