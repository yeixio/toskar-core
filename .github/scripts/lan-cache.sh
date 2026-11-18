#!/usr/bin/env bash
# Send this job's downloads through the LAN download cache (ops/lan-cache),
# when the repository variable CI_LAN_CACHE names the computer running it.
#
#   - name: Use the LAN download cache
#     run: "$GITHUB_WORKSPACE/.github/scripts/lan-cache.sh"
#     env:
#       CI_LAN_CACHE: ${{ vars.CI_LAN_CACHE }}
#
# CI_LAN_CACHE is the cache computer's address on the home network, such as
# 192.168.1.20 or linux-rig.lan, never localhost: the daemon's containers
# reach it too. Only self-hosted runners use it, and only when it answers,
# so a cache that is down or unset changes nothing. It sets, for later steps:
#
#   GOPROXY                  Go modules, falling back to proxy.golang.org
#   npm_config_registry      npm and pnpm packages
#   PLAYWRIGHT_DOWNLOAD_HOST Playwright's browsers
#   TOSKAR_DOWNLOAD_MIRROR   models a Toskar daemon downloads, falling back
#                            to Hugging Face
set -uo pipefail

host="${CI_LAN_CACHE:-}"
host="${host#http://}"
host="${host%%/*}"
[[ -n "$host" ]] || exit 0
if [[ "${RUNNER_ENVIRONMENT:-}" != "self-hosted" ]]; then
  echo "GitHub's machines aren't on the home network; not using the LAN cache."
  exit 0
fi

env_file="${GITHUB_ENV:-/dev/stdout}"
up() { curl -fsS -o /dev/null --max-time 3 "$1"; }

if up "http://$host:8380/healthz"; then
  {
    echo "GOPROXY=http://$host:8380/go|https://proxy.golang.org,direct"
    echo "PLAYWRIGHT_DOWNLOAD_HOST=http://$host:8380/playwright"
    echo "TOSKAR_DOWNLOAD_MIRROR=http://$host:8380"
  } >>"$env_file"
  echo "Go modules, Playwright's browsers and models come through the LAN cache at $host:8380."
else
  echo "::warning::The LAN download cache at $host:8380 didn't answer; downloading from the internet."
fi

# npm has no fallback of its own, so it is only used when it answers now.
if up "http://$host:4873/-/ping"; then
  echo "npm_config_registry=http://$host:4873/" >>"$env_file"
  echo "npm and pnpm packages come through the LAN cache at $host:4873."
else
  echo "::warning::The LAN npm cache at $host:4873 didn't answer; installing from the npm registry."
fi
exit 0
