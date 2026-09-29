#!/usr/bin/env bash
# Letterbox README screenshots onto the canvases listed in screenshots/config.json.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

CONFIG="${SCREENSHOT_CONFIG:-$ROOT/screenshots/config.json}"
RAW_DIR="$ROOT/docs/screenshots"
OUT_DIR="$ROOT/screenshots/appstore"

if [[ ! -f "$CONFIG" ]]; then
  echo "Missing screenshot config: $CONFIG" >&2
  exit 1
fi

if command -v magick >/dev/null 2>&1; then
  MAGICK=magick
elif command -v convert >/dev/null 2>&1; then
  MAGICK=convert
else
  echo "ImageMagick is required." >&2
  exit 1
fi

python3 - "$CONFIG" "$RAW_DIR" "$OUT_DIR" "$MAGICK" "$ROOT" <<'PY'
import json, os, subprocess, sys
config_path, raw_dir, out_dir, magick, root = sys.argv[1:6]
cfg = json.load(open(config_path))
background = cfg.get("background", "#121C26")
forms = cfg.get("forms") or [{
    "id": "desktop",
    "readme": True,
    "dimensions": cfg["dimensions"],
}]
wanted = [part.strip() for part in os.environ.get("SCREENSHOT_FORMS", "").split(",") if part.strip()]

def convert(source, dest, width, height):
    resize = [
        source,
        "-resize", f"{width}x{height}",
        "-background", background,
        "-gravity", "center",
        "-extent", f"{width}x{height}",
        "-alpha", "remove",
        "-alpha", "off",
    ]
    cmd = ["magick", *resize, f"png24:{dest}"] if magick == "magick" else ["convert", *resize, f"png24:{dest}"]
    subprocess.check_call(cmd)

for form in forms:
    if wanted and form["id"] not in wanted:
        continue
    source_dir = raw_dir if form.get("readme") else os.path.join(root, "screenshots/raw", form["id"])
    for size in form.get("dimensions") or []:
        width, height = (int(part) for part in size.split("x"))
        dest_dir = os.path.join(out_dir, size) if form.get("readme") else os.path.join(out_dir, form["id"], size)
        os.makedirs(dest_dir, exist_ok=True)
        for screen in cfg["screens"]:
            source = os.path.join(source_dir, screen["filename"])
            if not os.path.isfile(source):
                raise SystemExit(f"Missing screenshot: {source}")
            dest = os.path.join(dest_dir, screen["filename"])
            convert(source, dest, width, height)
            print(dest)
PY
