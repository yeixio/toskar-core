#!/usr/bin/env bash
# Picks the tag a pressed Release run ships, and checks it can ship. It pushes
# nothing: the Release workflow tags the commit only after its checks and the
# quality gate pass, so a failed run leaves no tag behind.
#
#   VERSION=1.16.0[-beta.1]  an exact version; empty means the newest
#                            version in CHANGELOG.md, which the Release PR
#                            (scripts/changelog.py release <version>) wrote
#   SHA=<commit>             the commit to release (HEAD by default)
#   REMOTE=origin            where the existing tags are read from
#
# Prints tag=<tag> and prerelease=true|false, to $GITHUB_OUTPUT when set.
# It works the same from a terminal, where it only reads.
set -euo pipefail

sha="${SHA:-$(git rev-parse HEAD)}"
remote="${REMOTE:-origin}"
changelog="${CHANGELOG:-CHANGELOG.md}"

fail() {
  echo "::error title=Not released::$*" >&2
  exit 1
}

# Spaces and a capital V, as phones type it, are fine: " V1.2.3" is v1.2.3.
version="$(printf '%s' "${VERSION:-}" | tr -d '[:space:]')"
version="${version#[vV]}"
if [ -z "$version" ]; then
  version="$(grep -m1 -oE '^## \[[0-9]+\.[0-9]+\.[0-9]+[^]]*\]' "$changelog" | sed 's/^## \[//; s/\]$//' || true)"
  [ -n "$version" ] || fail "$changelog has no released version yet. Merge the Release PR (scripts/changelog.py release <version>) first."
fi
tag="v$version"

[[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]] ||
  fail "$tag isn't a version like v1.2.3 or v1.2.3-beta.1."
prerelease=false
[[ "$tag" == *-* ]] && prerelease=true

# One line per release tag: "<tag> <commit>", peeled to the commit.
tags="$(git ls-remote --tags "$remote" 'v*' | awk '
  { name = $2; sub("^refs/tags/", "", name) }
  name ~ /\^\{\}$/ { sub(/\^\{\}$/, "", name); peeled[name] = $1; next }
  { direct[name] = $1 }
  END { for (n in direct) print n, (n in peeled ? peeled[n] : direct[n]) }')"

if grep -q "^$tag " <<<"$tags"; then
  fail "$tag already exists. Merge the next Release PR first, or give a new version."
fi

# A stable release ships what CHANGELOG.md says it ships.
if [ "$prerelease" = false ] && ! grep -qF "## [$version]" "$changelog"; then
  fail "$changelog has no section for $version. Merge its Release PR (scripts/changelog.py release $version) first."
fi

# A new version comes after the last stable one: v1.2.3-beta.1 is older than
# v1.2.3, so only its v1.2.3 part counts.
last="$(awk '{print $1}' <<<"$tags" | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | sort -V | tail -n1 || true)"
last="${last:-v0.0.0}"
core="${tag%%-*}"
newest="$(printf '%s\n%s\n' "$last" "$core" | sort -V | tail -n1)"
if [ "$core" = "$last" ] || [ "$newest" != "$core" ]; then
  fail "$tag doesn't come after the last release, $last."
fi

# Two stable versions on one commit ship the same thing twice. A pre-release
# followed by the stable version on the same commit is fine.
while read -r released commit; do
  [ -n "$released" ] || continue
  if [ "$commit" = "$sha" ] && [[ "$released" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    fail "${sha:0:7} is already released as $released. Merge something new first."
  fi
done <<<"$tags"

echo "Releasing ${sha:0:7} as $tag (last release $last)."
{
  echo "tag=$tag"
  echo "prerelease=$prerelease"
} | tee -a "${GITHUB_OUTPUT:-/dev/null}"
