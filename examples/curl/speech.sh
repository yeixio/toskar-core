#!/bin/sh
# Read text aloud with /v1/audio/speech, in the voice live voice speaks
# with (#1357), saved as speech.wav. Usage: speech.sh "Hello there."
# See ../README.md.
set -eu

base="${TOSKAR_URL:-http://127.0.0.1:7331}"
text="${1:-Hello from Toskar.}"
out="${TOSKAR_OUT:-speech.wav}"

json() {
  printf '%s' "$1" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g' -e 's/	/\\t/g' |
    awk 'NR > 1 { printf "\\n" } { printf "%s", $0 }'
}

body=$(printf '{"model":"tts-1","voice":"alloy","response_format":"wav","input":"%s"}' "$(json "$text")")
set -- -H "Content-Type: application/json"
[ -n "${TOSKAR_API_KEY:-}" ] && set -- "$@" -H "Authorization: Bearer $TOSKAR_API_KEY"
curl -fsS "$base/v1/audio/speech" "$@" -d "$body" -o "$out"
echo "$out"
