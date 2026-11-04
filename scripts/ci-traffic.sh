#!/usr/bin/env bash
#
# Report how much a job moved over a self-hosted runner's internet connection
# (#1081), so large downloads show up in the job summary.
#
#   scripts/ci-traffic.sh start [folder...]   note the counters and the folders' sizes
#   scripts/ci-traffic.sh report              add what changed to the job summary
#
# The counters are the whole computer's, so they include anything else it was
# doing. A folder's growth is what this job downloaded into it. GitHub's own
# machines aren't on that connection, so this does nothing there.
set -uo pipefail

[[ "${RUNNER_ENVIRONMENT:-}" == "self-hosted" ]] || exit 0

STATE="${RUNNER_TEMP:-${TMPDIR:-/tmp}}/ci-traffic"

# Prints bytes received and sent, leaving out loopback and container networks.
counters() {
  if [[ -r /proc/net/dev ]]; then
    awk -F'[: ]+' 'NR > 2 {
      name = ($1 == "") ? $2 : $1
      first = ($1 == "") ? 3 : 2
      if (name ~ /^(lo|docker|veth|br-|cni|flannel|tap|tun|virbr|kube)/) next
      rx += $first; tx += $(first + 8)
    } END { printf "%.0f %.0f\n", rx, tx }' /proc/net/dev
  elif command -v netstat >/dev/null 2>&1; then
    netstat -ibn | awk '$3 ~ /^<Link#/ && $1 ~ /^en[0-9]/ && !seen[$1]++ { rx += $7; tx += $10 } END { printf "%.0f %.0f\n", rx, tx }'
  fi
}

human() {
  awk -v b="$1" 'BEGIN { if (b < 0) b = 0; printf "%.2f GB", b / 1073741824 }'
}

folder_bytes() {
  [[ -d "$1" ]] || { echo 0; return; }
  du -sk "$1" 2>/dev/null | awk '{ printf "%.0f\n", $1 * 1024 }'
}

case "${1:-}" in
start)
  shift
  mkdir -p "$STATE"
  counters > "$STATE/counters"
  : > "$STATE/folders"
  for folder in "$@"; do
    printf '%s\t%s\n' "$(folder_bytes "$folder")" "$folder" >> "$STATE/folders"
  done
  ;;
report)
  [[ -s "$STATE/counters" ]] || exit 0
  read -r rx0 tx0 < "$STATE/counters"
  read -r rx1 tx1 < <(counters)
  {
    echo "### Network use"
    echo
    echo "This computer, while the job ran: $(human $((rx1 - rx0))) down, $(human $((tx1 - tx0))) up."
    while IFS=$'\t' read -r before folder; do
      echo "Downloaded into \`$folder\`: $(human $(($(folder_bytes "$folder") - before)))."
    done < "$STATE/folders"
  } >> "${GITHUB_STEP_SUMMARY:-/dev/stdout}"
  ;;
*)
  echo "usage: $0 start [folder...] | report" >&2
  exit 2
  ;;
esac
exit 0
