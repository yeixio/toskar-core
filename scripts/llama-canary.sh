#!/usr/bin/env bash
# The llama.cpp canary (#833, tests/canary): checks that a new llama.cpp
# release computes what the known-good tag does, on this computer's backend.
#
#   scripts/llama-canary.sh run       compare the newest release (or CANARY_NEW_TAG) with the good tag
#   scripts/llama-canary.sh refresh   write this lane's golden to tests/canary/goldens.json, for a reviewed PR
#   scripts/llama-canary.sh newest    print the newest release with this build
#   scripts/llama-canary.sh thinking  check thinking on and off per model family on the newest release (tests/thinking)
#
# CANARY_LANE names the lane in goldens.json, such as metal-arm64; CANARY_BUILD
# is the release build to test, as Toskar picks it (macos-arm64,
# ubuntu-vulkan-x64, ubuntu-x64, ...); CANARY_GPU=1 runs it on the GPU and
# also checks the CPU against it. Builds and the model are kept in
# CANARY_CACHE. gh must be signed in.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CACHE="${CANARY_CACHE:-$ROOT/.quality-cache}"
GOLDENS="$ROOT/tests/canary/goldens.json"
BUILD="${CANARY_BUILD:?set CANARY_BUILD, such as macos-arm64}"
mkdir -p "$CACHE"

golden() {
  python3 -c 'import json, sys; g = json.load(open(sys.argv[1]))
for k in sys.argv[2].split("."): g = g[k]
print(g)' "$GOLDENS" "$1"
}

# fetch TAG prints the directory holding that release's llama-perplexity.
fetch() {
  local tag="$1" dir="$CACHE/llama.cpp-$1-$BUILD" bin
  bin="$(find "$dir" -type f -name 'llama-perplexity*' 2>/dev/null | head -n 1 || true)"
  # Marks the build used, so the workflow's cleanup keeps it.
  [[ -d "$dir" ]] && touch "$dir"
  if [[ -z "$bin" ]]; then
    rm -rf "$dir"
    mkdir -p "$dir"
    gh release download "$tag" -R ggml-org/llama.cpp -p "llama-$tag-bin-$BUILD.*" -D "$dir" >&2
    for archive in "$dir"/*; do
      case "$archive" in
        *.zip) unzip -q "$archive" -d "$dir" && rm "$archive" ;;
        *.tar.gz | *.tgz) tar -xzf "$archive" -C "$dir" && rm "$archive" ;;
      esac
    done
    bin="$(find "$dir" -type f -name 'llama-perplexity*' | head -n 1)"
    if [[ -z "$bin" ]]; then
      echo "llama.cpp $tag's $BUILD build has no llama-perplexity" >&2
      return 1
    fi
    chmod +x "$bin"
  fi
  dirname "$bin"
}

# newest prints the newest release that has this build, as Toskar would install it.
newest() {
  gh api 'repos/ggml-org/llama.cpp/releases?per_page=20' \
    --jq "[.[] | select(any(.assets[]; .name | startswith(\"llama-\") and contains(\"-bin-$BUILD.\")))][0].tag_name"
}

sha256() {
  if command -v sha256sum >/dev/null; then sha256sum "$1"; else shasum -a 256 "$1"; fi | cut -d' ' -f1
}

model() {
  local url sum file
  url="$(golden model.url)"
  sum="$(golden model.sha256)"
  file="$CACHE/canary-$sum.gguf"
  if [[ ! -s "$file" ]]; then
    curl -fL --retry 3 -o "$file.part" "$url" >&2
    if [[ "$(sha256 "$file.part")" != "$sum" ]]; then
      rm -f "$file.part"
      echo "the canary model's SHA-256 isn't goldens.json's" >&2
      return 1
    fi
    mv "$file.part" "$file"
  fi
  echo "$file"
}

# thinking_models downloads tests/thinking/families.json's models into
# $CACHE/thinking, checked by SHA-256, and prints that folder.
thinking_models() {
  local dir="$CACHE/thinking" file url sum
  mkdir -p "$dir"
  while IFS=' ' read -r file url sum; do
    if [[ ! -s "$dir/$file" ]]; then
      curl -fL --retry 3 -o "$dir/$file.part" "$url" >&2
      if [[ "$(sha256 "$dir/$file.part")" != "$sum" ]]; then
        rm -f "$dir/$file.part"
        echo "$file's SHA-256 isn't families.json's" >&2
        return 1
      fi
      mv "$dir/$file.part" "$dir/$file"
    fi
  done < <(python3 -c 'import json, sys
for f in json.load(open(sys.argv[1]))["families"]: print(f["file"], f["url"], f["sha256"])' "$ROOT/tests/thinking/families.json")
  echo "$dir"
}

canary() {
  TOSKAR_CANARY_GOOD="$(fetch "$(golden good_tag)")"
  TOSKAR_CANARY_MODEL="$(model)"
  export TOSKAR_CANARY_GOOD TOSKAR_CANARY_MODEL
  export TOSKAR_CANARY_LANE="${CANARY_LANE:?set CANARY_LANE, such as metal-arm64}" TOSKAR_CANARY_GPU="${CANARY_GPU:-0}"
  export TOSKAR_CANARY_WORK="${TOSKAR_CANARY_WORK:-$CACHE}"
  cd "$ROOT"
  go test ./tests/canary -run TestLlamaCanary -count=1 -v -timeout 60m
}

case "${1:-run}" in
  run)
    tag="${CANARY_NEW_TAG:-$(newest)}"
    echo "llama.cpp canary on ${CANARY_LANE:-?}: $tag against $(golden good_tag)"
    TOSKAR_CANARY_NEW="$(fetch "$tag")"
    export TOSKAR_CANARY_NEW
    canary
    ;;
  refresh)
    TOSKAR_CANARY_UPDATE=1 canary
    ;;
  newest)
    newest
    ;;
  thinking)
    tag="${CANARY_NEW_TAG:-$(newest)}"
    echo "Thinking per family on llama.cpp $tag"
    TOSKAR_THINKING_LLAMA="$(fetch "$tag")"
    TOSKAR_THINKING_MODELS="$(thinking_models)"
    export TOSKAR_THINKING_LLAMA TOSKAR_THINKING_MODELS
    cd "$ROOT"
    go test ./tests/thinking -run TestThinkingFamilies -count=1 -v -timeout 30m
    ;;
  *)
    echo "usage: $0 run|refresh|newest|thinking" >&2
    exit 2
    ;;
esac
