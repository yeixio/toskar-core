#!/bin/sh
# Stream one answer from the OpenAI-compatible /v1/chat/completions, as
# server-sent events ending with data: [DONE]. Needs an installed model.
# See ../README.md.
set -eu

base="${TOSKAR_URL:-http://127.0.0.1:7331}"
model="${TOSKAR_MODEL:-auto}"
message="${1:-Hello}"

body=$(printf '{"model":"%s","stream":true,"messages":[{"role":"user","content":"%s"}]}' "$model" "$message")

if [ -n "${TOSKAR_API_KEY:-}" ]; then
  exec curl -fsSN "$base/v1/chat/completions" \
    -H "Authorization: Bearer $TOSKAR_API_KEY" \
    -H "Content-Type: application/json" \
    -d "$body"
fi

exec curl -fsSN "$base/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -d "$body"
