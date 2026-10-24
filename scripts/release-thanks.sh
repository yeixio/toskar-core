#!/usr/bin/env bash
# Prints a "Thanks to" line for the release notes: everyone with a commit
# between the previous release and TAG, except bots and the maintainers
# listed in MAINTAINERS.md. Prints nothing when there is nobody to thank.
#
# Usage: GH_TOKEN=... scripts/release-thanks.sh <owner/repo> <tag>
set -euo pipefail

repo="$1"
tag="$2"

prev="$(gh api "repos/$repo/releases?per_page=1" --jq '.[0].tag_name // empty')"
if [ -z "$prev" ] || [ "$prev" = "$tag" ]; then
  exit 0
fi

maintainers="$(awk '/^## Maintainers/ {f=1; next} /^## / {f=0} f' MAINTAINERS.md \
  | grep -o 'github.com/[A-Za-z0-9-]*' | cut -d/ -f2 | tr 'A-Z' 'a-z' || true)"

commits="$(gh api --paginate "repos/$repo/compare/$prev...$tag" \
  --jq '.commits[] | .author.login // empty')"
logins="$(printf '%s\n' "$commits" | grep -E '^[A-Za-z0-9-]+$' | sort -fu || true)"

thanks=()
while IFS= read -r login; do
  [ -n "$login" ] || continue
  if printf '%s\n' "$maintainers" | grep -qx "$(printf '%s' "$login" | tr 'A-Z' 'a-z')"; then
    continue
  fi
  thanks+=("@$login")
done <<< "$logins"

if [ "${#thanks[@]}" -eq 0 ]; then
  exit 0
fi

line="${thanks[0]}"
for ((i = 1; i < ${#thanks[@]}; i++)); do
  if [ "$i" -eq $((${#thanks[@]} - 1)) ]; then
    line="$line and ${thanks[$i]}"
  else
    line="$line, ${thanks[$i]}"
  fi
done
printf '## Thanks\n\nThanks to %s for contributing to this release.\n\n' "$line"
