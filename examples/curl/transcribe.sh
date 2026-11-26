#!/bin/sh
# Turn a recording into text with /v1/audio/transcriptions, using Whisper
# on this computer (#1357). Usage: transcribe.sh note.m4a [language]
# See ../README.md.
set -eu

base="${TOSKAR_URL:-http://127.0.0.1:7331}"
file="${1:?a recording, such as note.m4a}"

set -- -F "file=@$file" -F model=whisper-1 -F response_format=text ${2:+-F "language=$2"}
[ -n "${TOSKAR_API_KEY:-}" ] && set -- "$@" -H "Authorization: Bearer $TOSKAR_API_KEY"
exec curl -fsS "$base/v1/audio/transcriptions" "$@"
