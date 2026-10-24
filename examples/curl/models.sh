#!/bin/sh
# List what /v1/chat/completions takes as a model: auto, profiles, and
# specialized AIs. See ../README.md.
set -eu

base="${TOSKAR_URL:-http://127.0.0.1:7331}"

if [ -n "${TOSKAR_API_KEY:-}" ]; then
  exec curl -fsS "$base/v1/models" -H "Authorization: Bearer $TOSKAR_API_KEY"
fi

exec curl -fsS "$base/v1/models"
