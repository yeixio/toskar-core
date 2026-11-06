#!/bin/sh
# What this computer has for running AI: its CPU, memory, disk, and
# graphics. See ../README.md.
set -eu

base="${TOSKAR_URL:-http://127.0.0.1:7331}"

if [ -n "${TOSKAR_API_KEY:-}" ]; then
  exec curl -fsS "$base/api/v1/hardware" -H "Authorization: Bearer $TOSKAR_API_KEY"
fi

exec curl -fsS "$base/api/v1/hardware"
