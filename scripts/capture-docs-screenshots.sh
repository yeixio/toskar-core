#!/usr/bin/env bash
# Build the web UI and capture the user guide's screenshots
# (docs/user-guide/screens.json) into docs/user-guide/images/.
#
#   scripts/capture-docs-screenshots.sh            every screen
#   DOCS_SCREENS=chat scripts/capture-docs-screenshots.sh
#
# SCREENSHOT_CHROME uses an installed Chrome instead of downloading one.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
created_workspaces=()
cleanup_workspaces() {
  if ((${#created_workspaces[@]})); then
    rm -f "${created_workspaces[@]}"
  fi
}
trap cleanup_workspaces EXIT

install_pnpm() {
  local dir="$1"
  local major
  major="$(cd /tmp && pnpm --version | cut -d. -f1)"
  if [[ "$major" -ge 10 && ! -f "$dir/pnpm-workspace.yaml" ]]; then
    printf '%s\n' 'packages:' '  - "."' 'allowBuilds:' '  esbuild: true' '  playwright: true' > "$dir/pnpm-workspace.yaml"
    created_workspaces+=("$dir/pnpm-workspace.yaml")
  fi
  (cd "$dir" && pnpm install --frozen-lockfile)
}

# The screenshots show this checkout's interface, so the web UI is built
# from it every time.
install_pnpm web
(cd web && pnpm build)

install_pnpm scripts/screenshots
if [[ -z "${SCREENSHOT_CHROME:-}" ]]; then
  # A self-hosted runner has Chromium's system libraries and no sudo.
  if [[ "$(uname -s)" == "Linux" && "${RUNNER_ENVIRONMENT:-}" != "self-hosted" ]]; then
    (cd scripts/screenshots && pnpm exec playwright install --with-deps chromium)
  else
    (cd scripts/screenshots && pnpm exec playwright install chromium)
  fi
fi

node scripts/screenshots/docs.mjs
python3 scripts/check-user-guide.py
