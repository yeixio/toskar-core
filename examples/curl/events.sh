#!/bin/sh
# Follow Toskar's events as server-sent events: each chat's tokens as
# they're written, models loading, and more, as they happen, until Ctrl-C.
# Needs no installed model. See ../README.md.
set -eu

base="${TOSKAR_URL:-http://127.0.0.1:7331}"

if [ -n "${TOSKAR_API_KEY:-}" ]; then
  exec curl -fsSN "$base/api/v1/events" \
    -H "Accept: text/event-stream" \
    -H "Authorization: Bearer $TOSKAR_API_KEY"
fi

exec curl -fsSN "$base/api/v1/events" -H "Accept: text/event-stream"
