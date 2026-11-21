#!/usr/bin/env bash
# Keep the quality runner's Docker from filling its disk. The volume
# toskar-quality-models keeps every model any run ever installed, and every
# run's `docker build` leaves build cache, and nothing removed either: on
# linux-rig they reached 221G and 48G (2026-11).
#
# A model no run has read for QUALITY_MODEL_DAYS days (default 21) is
# removed; a run that needs it again downloads it again, through the LAN
# cache when it is up (ops/lan-cache). An image or video model is a folder,
# kept whole while any of its files was read. Build cache older than a week
# goes too. QUALITY_MODEL_DAYS=0 turns the model pruning off.
set -euo pipefail

days="${QUALITY_MODEL_DAYS:-21}"
[[ "$days" =~ ^[0-9]+$ ]] || { echo "::error::QUALITY_MODEL_DAYS must be a whole number."; exit 1; }

docker builder prune -af --filter until=168h >/dev/null || true

if [ "$days" = 0 ] || ! docker volume inspect toskar-quality-models >/dev/null 2>&1; then
  exit 0
fi

# GNU find, for -atime; the run pulls this image for its tests anyway.
docker run --rm --network none -e DAYS="$days" \
  -v toskar-quality-models:/m golang:1.26-bookworm bash -c '
    set -u
    before=$(du -sh /m | cut -f1)
    removed=0
    for e in /m/* /m/images/* /m/video/*; do
      [ -e "$e" ] || continue
      case "$e" in /m/images|/m/video|*/active) continue ;; esac
      if [ -z "$(find "$e" -type f -atime "-$DAYS" -print -quit)" ]; then
        echo "Removing ${e#/m/} ($(du -sh "$e" | cut -f1)), unread for $DAYS days"
        rm -rf "$e"
        removed=$((removed + 1))
      fi
    done
    # Downloads an interrupted run left half done.
    find /m/.partial -type f -mtime +1 -delete 2>/dev/null || true
    echo "Models volume: $before before, $(du -sh /m | cut -f1) after; $removed removed."
  ' | tee -a "${GITHUB_STEP_SUMMARY:-/dev/null}"
