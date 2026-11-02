#!/bin/sh
# Measure a local AI model on this computer, with Toskar's quality tests (#1037).
#
#   curl -fsSL https://toskar.ai/benchmark.sh | sh
#
# Downloads Toskar and its tests from the latest release, each checked against
# SHA256SUMS.txt, into one folder, without root, a service, Go, or Python. It runs
# its own throwaway Toskar, so your everyday Toskar and its chats aren't touched.
# It suggests a model and asks before downloading one. A real model runs with real
# tools, in an empty folder, and every tool that asks first is answered no. Nothing
# is sent anywhere unless you send the results form yourself.
#
# Environment:
#   TOSKAR_BENCHMARK_DIR   the folder (default ~/.toskar-benchmark); delete it to remove everything
#   TOSKAR_BENCHMARK_ARGS  more arguments, such as --model qwen2.5-3b-q4
#   TOSKAR_VERSION         a release such as 1.13.0 (default: the latest)
#   TOSKAR_RELEASE_URL     where the release files are (overrides the version)
set -eu

die() {
	printf 'Benchmark failed: %s\n' "$*" >&2
	exit 1
}

case "$(uname -s)" in
Linux) os="linux" ;;
Darwin) os="darwin" ;;
*) die "this script runs on Linux and macOS; on Windows, run: irm https://toskar.ai/benchmark.ps1 | iex" ;;
esac
case "$(uname -m)" in
x86_64 | amd64) arch="amd64" ;;
aarch64 | arm64) arch="arm64" ;;
*) die "this computer's processor ($(uname -m)) isn't one Toskar is built for" ;;
esac
command -v curl >/dev/null 2>&1 || die "this needs curl"
command -v tar >/dev/null 2>&1 || die "this needs tar"

if [ -n "${TOSKAR_RELEASE_URL:-}" ]; then
	base="${TOSKAR_RELEASE_URL%/}"
elif [ -n "${TOSKAR_VERSION:-}" ]; then
	base="https://github.com/yeixio/toskar-core/releases/download/v${TOSKAR_VERSION#v}"
else
	base="https://github.com/yeixio/toskar-core/releases/latest/download"
fi

sha256() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | awk '{print $1}'
	else
		shasum -a 256 "$1" | awk '{print $1}'
	fi
}

dir="${TOSKAR_BENCHMARK_DIR:-$HOME/.toskar-benchmark}"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$dir/results"

curl -fsSL "$base/SHA256SUMS.txt" -o "$tmp/SHA256SUMS.txt" || die "couldn't download the release list from $base"

# unpack <pattern> <folder>: one release file, checked, in <folder> without its top level.
unpack() {
	line="$(grep -E "  ($1)\$" "$tmp/SHA256SUMS.txt" | head -n 1 || true)"
	[ -n "$line" ] || die "this release has no file for this computer ($os $arch), or was made before the benchmark"
	want="${line%% *}"
	name="${line##* }"
	printf 'Downloading %s...\n' "$name"
	curl -fsSL "$base/$name" -o "$tmp/$name" || die "couldn't download $base/$name"
	[ "$(sha256 "$tmp/$name")" = "$want" ] || die "$name doesn't match its checksum, so it wasn't used"
	rm -rf "$2"
	mkdir -p "$2"
	tar -xzf "$tmp/$name" -C "$2" --strip-components=1 || die "couldn't unpack $name"
	if [ "$os" = "darwin" ]; then xattr -dr com.apple.quarantine "$2" 2>/dev/null || true; fi
}

unpack "toskar-[0-9][^ ]*-${os}-${arch}-headless\.tar\.gz" "$dir/toskar"
unpack "toskar-quality-[^ ]*-${os}-${arch}\.tar\.gz" "$dir/quality"

cd "$dir/results"
# shellcheck disable=SC2086 # the arguments are meant to split
"$dir/quality/toskar-quality" run --toskar "$dir/toskar/toskar" --harness "$dir/quality" --data "$dir/data" ${TOSKAR_BENCHMARK_ARGS:-}
