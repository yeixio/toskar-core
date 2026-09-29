#!/usr/bin/env bash
# Check that screenshots/appstore contains a PNG of each configured screen and size.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

CONFIG="${SCREENSHOT_CONFIG:-$ROOT/screenshots/config.json}"
OUT_DIR="$ROOT/screenshots/appstore"

if command -v magick >/dev/null 2>&1; then
  MAGICK=magick
elif command -v identify >/dev/null 2>&1; then
  MAGICK=identify
else
  echo "ImageMagick is required to validate dimensions." >&2
  exit 1
fi

python3 - "$CONFIG" "$OUT_DIR" "$MAGICK" <<'PY'
import json, os, subprocess, sys
config_path, out_dir, magick = sys.argv[1:4]
cfg = json.load(open(config_path))
errors = []
forms = cfg.get("forms") or [{
    "id": "desktop",
    "readme": True,
    "dimensions": cfg["dimensions"],
}]
wanted = [part.strip() for part in os.environ.get("SCREENSHOT_FORMS", "").split(",") if part.strip()]

def identify(path):
    fmt = "%w %h %[opaque]"
    cmd = [magick, "identify", "-format", fmt, path] if magick == "magick" else ["identify", "-format", fmt, path]
    return subprocess.check_output(cmd, text=True).split()

def check_canvas(label, size, directory):
    want_w, want_h = (int(part) for part in size.split("x"))
    names = []
    for screen in cfg["screens"]:
        filename = screen["filename"]
        names.append(filename)
        path = os.path.join(directory, filename)
        if not os.path.isfile(path):
            errors.append(f"Missing screenshot: {path}")
            continue
        with open(path, "rb") as handle:
            header = handle.read(8)
        if header != b"\x89PNG\r\n\x1a\n":
            errors.append(f"Not a PNG: {path}")
        if os.path.getsize(path) < 10000:
            errors.append(f"Screenshot looks empty: {path}")
            continue
        got_w, got_h, opaque = identify(path)
        if int(got_w) != want_w or int(got_h) != want_h:
            errors.append(f"{path} is {got_w}x{got_h}, expected {want_w}x{want_h}")
        if opaque != "True":
            errors.append(f"{path} has an alpha channel")
    if names != sorted(names):
        errors.append(f"Filenames for {label} are not in numeric order: {', '.join(names)}")
    return names

previous = []
for form in forms:
    if wanted and form["id"] not in wanted:
        continue
    for size in form.get("dimensions") or []:
        directory = os.path.join(out_dir, size) if form.get("readme") else os.path.join(out_dir, form["id"], size)
        label = size if form.get("readme") else f"{form['id']} {size}"
        names = check_canvas(label, size, directory)
        if previous and names and previous != names:
            errors.append(f"Filenames for {label} do not match the previous canvas")
        if names:
            previous = names
if errors:
    print("\n".join(errors), file=sys.stderr)
    raise SystemExit("Screenshot validation failed")
print(f"Screenshot validation passed: {out_dir}")
PY
