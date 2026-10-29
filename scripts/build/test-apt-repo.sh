#!/usr/bin/env bash
# Tests Toskar's apt repository the way people add it (#599): a fresh Ubuntu
# container runs the user guide's lines (#885), which add it with its key,
# never with [trusted=yes], and install from it, with no warnings.
#
#   test-apt-repo.sh            builds a small package, signs a repository
#                               with a key made for the test, and installs it
#   APT_TEST_DEBS=dist test-apt-repo.sh
#                               the same with the toskar packages in dist/
#   test-apt-repo.sh <dir>      tests a repository already built and signed,
#                               such as the release's dist/apt, installing toskar
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
IMAGE="${APT_TEST_IMAGE:-buildpack-deps:noble-curl}"
work="$(mktemp -d)"
server=""
cleanup() {
  if [ -n "$server" ]; then
    kill "$server" 2>/dev/null || true
  fi
  rm -rf "$work"
}
trap cleanup EXIT

if [ $# -ge 1 ]; then
  repo="$(cd "$1" && pwd)"
  package="toskar"
else
  mkdir -p "$work/debs"
  if [ -n "${APT_TEST_DEBS:-}" ]; then
    package="toskar"
    cp "$APT_TEST_DEBS"/*.deb "$work/debs/"
  else
    # A package with nothing in it.
    package="toskar-apt-test"
    mkdir -p "$work/pkg/DEBIAN"
    printf 'Package: %s\nVersion: 1.0\nArchitecture: all\nMaintainer: Toskar <hello@yeix.io>\nDescription: apt repository test\n' "$package" > "$work/pkg/DEBIAN/control"
    dpkg-deb --build --root-owner-group "$work/pkg" "$work/debs/${package}_1.0_all.deb" >/dev/null
  fi
  # A key made for this test only.
  export GNUPGHOME="$work/gnupg"
  mkdir -m 700 "$GNUPGHOME"
  gpg --batch --quiet --pinentry-mode loopback --passphrase test --quick-generate-key "Toskar apt test <apt-test@toskar.invalid>" ed25519 sign 1d
  key="$(gpg --batch --pinentry-mode loopback --passphrase test --armor --export-secret-keys)"
  unset GNUPGHOME
  repo="$work/apt"
  APT_DEBS="$work/debs" APT_SIGNING_KEY="$key" APT_SIGNING_PASSPHRASE=test "$ROOT/scripts/build/apt-index.sh" "$repo"
fi

for f in dists/stable/InRelease dists/stable/Release.gpg toskar.asc; do
  if [ ! -s "$repo/$f" ]; then
    echo "The repository has no $f: it isn't signed." >&2
    exit 1
  fi
done

port="$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1])')"
python3 -m http.server "$port" --bind 127.0.0.1 --directory "$repo" >/dev/null 2>&1 &
server=$!
sleep 1

# The user guide's own lines (install-apt in docs/user-guide/guide.json),
# with this test's address in place of toskar.ai's and GitHub's, run as a
# reader runs them. Only this repository, so the test doesn't depend on
# Ubuntu's mirrors.
guide="$(python3 "$ROOT/scripts/guide-example.py" install-apt)"
lines="${guide//https:\/\/toskar.ai\/apt\/toskar.asc/http://127.0.0.1:$port/toskar.asc}"
lines="${lines//https:\/\/raw.githubusercontent.com\/yeixio\/toskar-core\/apt/http://127.0.0.1:$port}"
lines="${lines//apt-get install toskar/apt-get install $package}"
if [ "$lines" = "$guide" ] || printf '%s' "$lines" | grep -q 'https://'; then
  echo "The guide's apt lines changed: point this test at their new addresses." >&2
  exit 1
fi
# curl is in the guide's lines, and Ubuntu's base image has none: this one
# is Ubuntu 24.04 with curl.
# shellcheck disable=SC2016 # the script runs in the container
docker run --rm --network host -e DEBIAN_FRONTEND=noninteractive -e GUIDE_LINES="$lines" -e PACKAGE="$package" "$IMAGE" bash -euo pipefail -c '
  rm -f /etc/apt/sources.list /etc/apt/sources.list.d/*
  # A reader answers yes when apt asks, and is root by way of sudo.
  echo "APT::Get::Assume-Yes \"true\";" > /etc/apt/apt.conf.d/90yes
  sudo() { "$@"; }
  { eval "$GUIDE_LINES"; } 2>&1 | tee /tmp/run.log
  if grep -Eq "^(W|E|Err):" /tmp/run.log; then
    echo "apt warned or failed." >&2
    exit 1
  fi
  if grep -q "trusted=yes" /etc/apt/sources.list.d/toskar.list; then
    echo "The repository was added without its key." >&2
    exit 1
  fi
  dpkg -s "$PACKAGE" >/dev/null
'
echo "The apt repository is signed, and apt installs $package from it."
