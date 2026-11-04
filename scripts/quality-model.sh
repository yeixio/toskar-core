#!/usr/bin/env bash
# Runs the quality set (tests/quality) against a real model, on CPU.
#
#   scripts/quality-model.sh serve   start llama-server with the model, in the foreground
#   scripts/quality-model.sh run     start it, run core's cases against it, and stop it
#
# TOSKAR_QUALITY_RUN picks the tests `run` runs (go test -run); the default
# is the quality set alone, as a release runs it.
#
# llama.cpp comes from its GitHub release LLAMA_CPP_TAG (gh must be signed
# in), and the model from QUALITY_MODEL_GGUF_URL, both kept in
# QUALITY_CACHE. The default model is Llama 3.2 1B, the smallest the
# iPhone app offers, so a release is held to what the weakest model can do.
# The iPhone app's quality run (yeixio/toskar-apps) uses `serve`.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
# A self-hosted computer keeps these in one folder for all its runners, so it
# downloads each one once (#1081). GitHub's machines restore .quality-cache.
if [[ "${RUNNER_ENVIRONMENT:-}" == "self-hosted" ]]; then
  CACHE="${QUALITY_CACHE:-$HOME/.cache/toskar-quality-model}"
else
  CACHE="${QUALITY_CACHE:-$ROOT/.quality-cache}"
fi
TAG="${LLAMA_CPP_TAG:-b11174}"
MODEL_URL="${QUALITY_MODEL_GGUF_URL:-https://huggingface.co/bartowski/Llama-3.2-1B-Instruct-GGUF/resolve/main/Llama-3.2-1B-Instruct-Q4_K_M.gguf}"
PORT="${QUALITY_MODEL_PORT:-8089}"
CTX="${QUALITY_MODEL_CTX:-4096}"
mkdir -p "$CACHE"
# What no run has used for a while: an older llama.cpp, a model no longer tested.
find "$CACHE" -mindepth 1 -maxdepth 1 -name 'llama.cpp-*' ! -name "llama.cpp-$TAG" -mtime +14 -exec rm -rf {} +
find "$CACHE" -mindepth 1 -maxdepth 1 \( -name '*.gguf' -o -name '*.gguf.sha256' \) -mtime +30 -exec rm -f {} +
find "$CACHE" -mindepth 1 -maxdepth 1 -name '*.part' -mtime +2 -exec rm -f {} +

sha256() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

llama_server() {
  local dir="$CACHE/llama.cpp-$TAG"
  local bin
  bin="$(find "$dir" -type f -name llama-server 2>/dev/null | head -n 1 || true)"
  if [[ -z "$bin" ]]; then
    # Unpacked beside the folder and moved in whole, since another runner on
    # this computer may be doing the same.
    local work="$dir.$$"
    rm -rf "$work"
    mkdir -p "$work"
    gh release download "$TAG" -R ggml-org/llama.cpp -p "*bin-ubuntu-x64.*" -D "$work" >&2
    for archive in "$work"/*; do
      case "$archive" in
        *.zip) unzip -q "$archive" -d "$work" ;;
        *.tar.gz | *.tgz) tar -xzf "$archive" -C "$work" ;;
      esac
    done
    chmod +x "$(find "$work" -type f -name llama-server | head -n 1)"
    # rename(2) refuses a folder that's already there, so the first one in wins;
    # mv would move this one inside it.
    perl -e 'exit !rename($ARGV[0], $ARGV[1])' "$work" "$dir" || rm -rf "$work"
    bin="$(find "$dir" -type f -name llama-server | head -n 1)"
  fi
  touch "$dir"
  echo "$bin"
}

model() {
  local file="$CACHE/$(basename "${MODEL_URL%%\?*}")"
  # A kept file that no longer matches its checksum is downloaded again.
  if [[ -s "$file" && -s "$file.sha256" && "$(sha256 "$file")" != "$(cat "$file.sha256")" ]]; then
    echo "$(basename "$file") doesn't match its checksum, so it's downloaded again." >&2
    rm -f "$file" "$file.sha256"
  fi
  if [[ ! -s "$file" ]]; then
    curl -fL --retry 3 -o "$file.$$.part" "$MODEL_URL" >&2
    mv "$file.$$.part" "$file"
    rm -f "$file.sha256"
  fi
  if [[ ! -s "$file.sha256" ]]; then
    sha256 "$file" > "$file.sha256.$$"
    mv "$file.sha256.$$" "$file.sha256"
  fi
  touch "$file" "$file.sha256"
  echo "$file"
}

serve() {
  local bin gguf
  bin="$(llama_server)"
  gguf="$(model)"
  # The release archive keeps its shared libraries beside llama-server.
  export LD_LIBRARY_PATH="$(dirname "$bin")${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
  exec "$bin" -m "$gguf" -c "$CTX" --host 127.0.0.1 --port "$PORT" --jinja -np 1
}

wait_ready() {
  for _ in $(seq 1 180); do
    if curl -fs "http://127.0.0.1:$PORT/health" >/dev/null 2>&1; then
      return 0
    fi
    sleep 2
  done
  echo "llama-server did not start" >&2
  return 1
}

case "${1:-run}" in
  serve)
    serve
    ;;
  wait)
    wait_ready
    ;;
  run)
    serve > "$CACHE/llama-server.log" 2>&1 &
    pid=$!
    trap 'kill "$pid" 2>/dev/null || true' EXIT
    wait_ready || { tail -n 50 "$CACHE/llama-server.log" >&2; exit 1; }
    cd "$ROOT"
    TOSKAR_QUALITY_MODEL_URL="http://127.0.0.1:$PORT" \
      TOSKAR_QUALITY_REPORT="${TOSKAR_QUALITY_REPORT:-$CACHE/quality-report.md}" \
      go test ./tests/quality -run "${TOSKAR_QUALITY_RUN:-TestQualitySet}" -count=1 -v -timeout 120m
    ;;
  *)
    echo "usage: $0 serve|wait|run" >&2
    exit 2
    ;;
esac
