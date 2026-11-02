#!/usr/bin/env bash
# Builds a local release of what the one-line benchmark downloads (#1037), and
# serves it, for CI and for trying it. Prints the address, and keeps serving
# until it's stopped.
#
#   scripts/test-benchmark.sh <folder> <port> [<goos> <goarch>]
set -euo pipefail
out="$(mkdir -p "$1" && cd "$1" && pwd)"
port="$2"
goos="${3:-$(go env GOOS)}"
goarch="${4:-$(go env GOARCH)}"
cd "$(dirname "$0")/.."

ext=""
if [ "$goos" = windows ]; then ext=".exe"; fi
export CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch"
stage="$out/toskar-0.0.0-${goos}-${goarch}-headless"
quality="$out/toskar-quality-0.0.0-${goos}-${goarch}"
mkdir -p "$stage" "$quality"
go build -o "$stage/toskar${ext}" ./cmd/daemon
go build -o "$quality/toskar-quality${ext}" ./cmd/qualityrun
go test -c -o "$quality/quality.test${ext}" ./tests/quality
cp tests/quality/*.json "$quality/"
(
  cd "$out"
  tar -czf "${stage##*/}.tar.gz" "${stage##*/}"
  tar -czf "${quality##*/}.tar.gz" "${quality##*/}"
  rm -rf "$stage" "$quality"
  : > SHA256SUMS.txt
  for f in *.tar.gz; do
    if command -v sha256sum >/dev/null 2>&1; then
      printf '%s  %s\n' "$(sha256sum "$f" | awk '{print $1}')" "$f" >> SHA256SUMS.txt
    else
      printf '%s  %s\n' "$(shasum -a 256 "$f" | awk '{print $1}')" "$f" >> SHA256SUMS.txt
    fi
  done
)
unset CGO_ENABLED GOOS GOARCH
echo "Serving http://127.0.0.1:${port}"
cd "$out"
exec python3 -m http.server "$port" --bind 127.0.0.1
