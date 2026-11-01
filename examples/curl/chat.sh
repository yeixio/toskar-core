#!/bin/sh
# Stream one answer from the OpenAI-compatible /v1/chat/completions, as
# server-sent events ending with data: [DONE]. Needs an installed model.
# See ../README.md.
set -eu

base="${TOSKAR_URL:-http://127.0.0.1:7331}"
model="${TOSKAR_MODEL:-auto}"
message="${1:-Hello}"

# A JSON string: backslashes and quotes escaped, tabs and newlines as \t and \n.
json() {
  printf '%s' "$1" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g' -e 's/	/\\t/g' |
    awk 'NR > 1 { printf "\\n" } { printf "%s", $0 }'
}

body=$(printf '{"model":"%s","stream":true,"messages":[{"role":"user","content":"%s"}]}' "$(json "$model")" "$(json "$message")")

if [ -n "${TOSKAR_API_KEY:-}" ]; then
  exec curl -fsSN "$base/v1/chat/completions" \
    -H "Authorization: Bearer $TOSKAR_API_KEY" \
    -H "Content-Type: application/json" \
    -d "$body"
fi

exec curl -fsSN "$base/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -d "$body"
