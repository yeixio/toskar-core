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
out="$PWD/quality-score-$model"
port="${TOSKAR_QUALITY_PORT:-7361}"
work="$(mktemp -d)"
mkdir -p "$data" "$out"
url="http://127.0.0.1:$port"

cd "$root"
go build -o "$work/toskar" ./cmd/daemon

(
  cd "$work"
  TOSKAR_API_PORT="$port" TOSKAR_INTERNAL_PORT="$((port + 1))" TOSKAR_DISCOVERY_ENABLED=false \
    TOSKAR_WEB_FIXTURES="$root/tests/quality/web.json" \
    exec ./toskar -data-dir "$data" >"$out/toskar.log" 2>&1
) &
toskar=$!
trap 'kill "$toskar" 2>/dev/null || true; rm -rf "$work"' EXIT

for _ in $(seq 1 120); do
  curl -sf "$url/api/v1/health" >/dev/null && break
  sleep 0.5
done
curl -sf "$url/api/v1/health" >/dev/null || { echo "Toskar didn't start; see $out/toskar.log" >&2; exit 1; }

echo "Measuring $model. This can take a long time, and logs go to $out/run.log."
# A set below its bar still has a result to report, so its failure doesn't stop this.
TOSKAR_QUALITY_URL="$url" TOSKAR_QUALITY_MODEL="$model" \
  TOSKAR_QUALITY_REPORT="$out/quality.md" TOSKAR_QUALITY_TOPIC_REPORT="$out/topics.md" \
  TOSKAR_QUALITY_FILES_REPORT="$out/files.md" TOSKAR_QUALITY_REASONING_REPORT="$out/reasoning.md" \
  go test ./tests/quality -run '^(TestQualitySet|TestTopicQuality|TestFilesQuality|TestReasoningQuality)$' -count=1 -v -timeout 600m \
  >"$out/run.log" 2>&1 || true

hardware="$(curl -s "$url/api/v1/hardware")"
python3 "$root/scripts/quality_result.py" "$model" "$out" "$root/tests/quality/version.json" "$hardware"
