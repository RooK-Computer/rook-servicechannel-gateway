#!/bin/sh
set -eu

if command -v systemctl >/dev/null 2>&1; then
  if systemctl is-active --quiet rook-servicechannel-gateway.service; then
    systemctl stop rook-servicechannel-gateway.service || true
  fi
fi
