#!/usr/bin/env bash
# Measures one model with Toskar's quality sets, on this computer, and writes
# the result to submit (docs/quality.md).
#
#   scripts/quality-score.sh <model id>     such as qwen2.5-7b-q4
#
# It runs its own Toskar, built from this checkout, with its own data folder and
# ports, in an empty folder, so your everyday Toskar and its chats aren't
# touched. Web lookups answer from tests/quality/web.json, and every tool that
# asks first is answered no. Models stay in its data folder between runs:
# TOSKAR_QUALITY_DATA moves it, from ~/.cache/toskar-quality.
#
# It takes from minutes to hours, depending on the model and the computer.
set -euo pipefail

model="${1:?usage: scripts/quality-score.sh <model id>; toskarctl models list shows them}"
root="$(cd "$(dirname "$0")/.." && pwd)"
data="${TOSKAR_QUALITY_DATA:-$HOME/.cache/toskar-quality}"
start="$PWD"
port="${TOSKAR_QUALITY_PORT:-7361}"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/harness"

cd "$root"
go build -o "$work/toskar" ./cmd/daemon
go build -o "$work/toskar-quality" ./cmd/qualityrun
go test -c -o "$work/harness/quality.test" ./tests/quality
cp tests/quality/*.json "$work/harness/"
cp -R tests/quality/fixtures "$work/harness/fixtures"
cp internal/mimir/testdata/scanned.pdf "$work/harness/fixtures/"
cd "$start"
"$work/toskar-quality" run --model "$model" --toskar "$work/toskar" --harness "$work/harness" \
  --out "$start/quality-score-$model" --data "$data" --port "$port"
