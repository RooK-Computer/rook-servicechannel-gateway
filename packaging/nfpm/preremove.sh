#!/bin/sh
set -eu

if command -v systemctl >/dev/null 2>&1; then
  if systemctl is-enabled --quiet rook-servicechannel-gateway.service 2>/dev/null; then
    systemctl disable rook-servicechannel-gateway.service || true
  fi
  if systemctl is-active --quiet rook-servicechannel-gateway.service; then
    systemctl stop rook-servicechannel-gateway.service || true
  fi
fi
