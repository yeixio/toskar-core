#!/bin/sh
# Embed text with /v1/embeddings, using the embedding model knowledge
# search uses (#1357). Prints the JSON. Needs an embedding model.
# See ../README.md.
set -eu

base="${TOSKAR_URL:-http://127.0.0.1:7331}"
text="${1:-Toskar runs on your own computer.}"

json() {
  printf '%s' "$1" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g' -e 's/	/\\t/g' |
    awk 'NR > 1 { printf "\\n" } { printf "%s", $0 }'
}

body=$(printf '{"model":"text-embedding-3-small","input":"%s"}' "$(json "$text")")
set -- -H "Content-Type: application/json"
[ -n "${TOSKAR_API_KEY:-}" ] && set -- "$@" -H "Authorization: Bearer $TOSKAR_API_KEY"
exec curl -fsS "$base/v1/embeddings" "$@" -d "$body"
