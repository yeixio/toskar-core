#!/usr/bin/env python3
"""Check the user guide against docs/user-guide/STYLE.md.

Every feature gets a detailed page on toskar.ai/docs (yeixio/toskar-core#882).
A section is detailed once it has blocks; these checks hold every detailed
section to the bar, and every section to the parts that apply to all of them.

Usage:
    scripts/check-user-guide.py                      check docs/user-guide/guide.json
    scripts/check-user-guide.py pr --base <ref>      check that a pull request that
                                                     adds a feature (an ### Added
                                                     changelog fragment) changes the
                                                     guide, unless it has the
                                                     no-user-docs label (LABELS env,
                                                     comma separated)
"""

from __future__ import annotations

import argparse
import importlib.util
import json
import os
import re
import struct
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
GUIDE = ROOT / "docs" / "user-guide" / "guide.json"

spec = importlib.util.spec_from_file_location("publisher", Path(__file__).with_name("publish-docs.py"))
publisher = importlib.util.module_from_spec(spec)
spec.loader.exec_module(publisher)

# User text says this device, phone, tablet, TV, or watch, never a brand.
BRANDS = re.compile(r"\b(iPhone|iPad|iPod|iOS|iPadOS|Android|Apple TV|Apple Watch|watchOS|tvOS|Wear OS|Galaxy|Pixel)\b")
SENTENCE = re.compile(r"[^.!?]+[.!?]")
# A sentence a reader has to read twice; the average stays well under it.
LONGEST_SENTENCE = 45
AVERAGE_SENTENCE = 25
MIN_IMAGE_WIDTH = 640
NO_DOCS_LABEL = "no-user-docs"
# The languages examples/test.py runs a tested example in.
RUNNABLE = ("sh", "python", "typescript", "go")


def png_size(path: Path) -> tuple[int, int] | None:
    data = path.read_bytes()[:24]
    if len(data) < 24 or data[:8] != b"\x89PNG\r\n\x1a\n" or data[12:16] != b"IHDR":
        return None
    return struct.unpack(">II", data[16:24])


def words(sentence: str) -> int:
    return len(sentence.split())


def plain_text(value: str) -> str:
    """Prose without inline code or link targets, for sentence lengths."""
    value = re.sub(r"`[^`]*`", "code", value)
    return publisher.LINK.sub(lambda m: m.group(1), value)


def check(guide: dict, root: Path = ROOT) -> tuple[list[str], list[str]]:
    """Problems that fail the check, and sections not yet detailed.

    Screenshots a page names come from docs/user-guide/screens.json; a screen
    no page uses, or a PNG in docs/user-guide/images that no page shows, is
    left over and fails too.
    """
    problems: list[str] = []
    try:
        publisher.validate(guide)
    except ValueError as error:
        return [f"guide.json: {error}"], []
    tests: dict[str, str] = {}
    waiting: list[str] = []
    screens_file = root / "docs" / "user-guide" / "screens.json"
    screens = {s["id"] for s in json.loads(screens_file.read_text())["screens"]} if screens_file.is_file() else set()
    used_screens: set[str] = set()
    used_files: set[str] = set()
    for section in guide["sections"]:
        sid = section["id"]
        where = f"{sid}:"
        for value in publisher.inline_texts(section):
            if found := BRANDS.search(value):
                problems.append(f"{where} says {found.group(0)!r}; say this device, phone, tablet, TV, or watch")
        images = section.get("images", []) + [b for b in section.get("blocks", []) if b["type"] == "image"]
        for image in images:
            used_files.add(image["src"])
            if "screen" in image:
                used_screens.add(image["screen"])
                if image["screen"] not in screens:
                    problems.append(f"{where} screen {image['screen']!r} isn't in docs/user-guide/screens.json")
                    continue
            elif section.get("blocks"):
                problems.append(f"{where} {image['src']} is hand-made; a detailed page's screenshots name a screen in screens.json, so CI captures them")
            path = root / image["src"]
            if not path.is_file():
                problems.append(f"{where} {image['src']} is missing")
                continue
            size = png_size(path)
            if size is None:
                problems.append(f"{where} {image['src']} isn't a PNG")
            elif size[0] < MIN_IMAGE_WIDTH:
                problems.append(f"{where} {image['src']} is {size[0]} px wide; screenshots are at least {MIN_IMAGE_WIDTH}")
        blocks = section.get("blocks", [])
        for block in blocks:
            if block["type"] == "code" and "test" in block:
                if block["test"] in tests:
                    problems.append(f"{where} example {block['test']!r} is also in {tests[block['test']]}")
                tests[block["test"]] = sid
                if block["language"] not in RUNNABLE:
                    problems.append(f"{where} example {block['test']!r} is {block['language']}; examples/test.py runs {', '.join(RUNNABLE)}")
        if not blocks:
            waiting.append(sid)
            continue
        # A detailed section: the bar in STYLE.md.
        if "summary" not in section:
            problems.append(f"{where} a detailed section needs a one-line summary")
        if "kind" not in section:
            problems.append(f"{where} a detailed section needs a kind ({', '.join(publisher.KINDS)})")
        shown = any(b["type"] == "image" for b in blocks) or any(b["type"] == "code" and "test" in b for b in blocks)
        if not shown:
            problems.append(f"{where} needs a screenshot or a tested example")
        if section.get("kind") == "task":
            titles = {b.get("title", "").lower() for b in blocks if b["type"] == "list"}
            headings = {b["text"].lower() for b in blocks if b["type"] == "heading"}
            if "what you need" not in titles:
                problems.append(f"{where} a task starts with a 'What you need' list")
            if "troubleshooting" not in headings:
                problems.append(f"{where} a task ends with a 'Troubleshooting' heading")
        prose = [section["summary"]] if "summary" in section else []
        prose += section["paragraphs"]
        prose += [b["text"] for b in blocks if b["type"] in ("paragraph", "note")]
        prose += [item for b in blocks if b["type"] in ("steps", "list") for item in b["items"]]
        sentences = [s for p in prose for s in SENTENCE.findall(plain_text(p)) if s.strip()]
        for sentence in sentences:
            if words(sentence) > LONGEST_SENTENCE:
                problems.append(f"{where} a {words(sentence)}-word sentence; split it: {sentence.strip()[:80]}…")
        if sentences and sum(map(words, sentences)) / len(sentences) > AVERAGE_SENTENCE:
            problems.append(f"{where} sentences average over {AVERAGE_SENTENCE} words; write shorter ones")
    for screen in sorted(screens - used_screens):
        problems.append(f"screens.json: {screen!r} isn't shown on any page; remove it or use it")
    images_dir = root / "docs" / "user-guide" / "images"
    if images_dir.is_dir():
        for png in sorted(images_dir.rglob("*.png")):
            relative = png.relative_to(root).as_posix()
            if relative not in used_files:
                problems.append(f"{relative} isn't shown on any page; remove it")
    return problems, waiting


def added_features(base: str) -> list[str]:
    """Changelog fragments this branch adds or changes that announce a feature."""
    names = subprocess.run(
        ["git", "diff", "--name-only", "--diff-filter=AM", f"{base}...HEAD", "--", "changes/unreleased/"],
        cwd=ROOT, capture_output=True, text=True, check=True,
    ).stdout.split()
    return [n for n in names if n.endswith(".md") and not n.endswith("README.md") and "### Added" in (ROOT / n).read_text()]


def guide_changed(base: str) -> bool:
    out = subprocess.run(
        ["git", "diff", "--name-only", f"{base}...HEAD", "--", "docs/user-guide/"],
        cwd=ROOT, capture_output=True, text=True, check=True,
    ).stdout
    return bool(out.strip())


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = parser.add_subparsers(dest="command")
    pr = sub.add_parser("pr", help="a pull request that adds a feature changes the guide")
    pr.add_argument("--base", required=True)
    args = parser.parse_args()
    if args.command == "pr":
        labels = {label.strip() for label in os.environ.get("LABELS", "").split(",") if label.strip()}
        features = added_features(args.base)
        if not features or guide_changed(args.base):
            print("User docs: nothing to add, or the guide changed with the feature.")
            return 0
        if NO_DOCS_LABEL in labels:
            print(f"User docs: {', '.join(features)} add a feature; the {NO_DOCS_LABEL} label says why there's no page.")
            return 0
        print(f"{', '.join(features)} add a feature (### Added), but docs/user-guide/ didn't change.")
        print("Every feature ships its page in the user guide (docs/user-guide/STYLE.md).")
        print(f"If this one needs none, such as an API-only change already covered, add the {NO_DOCS_LABEL} label and say why.")
        return 1
    problems, waiting = check(json.loads(GUIDE.read_text()))
    for problem in problems:
        print(problem)
    detailed = len(json.loads(GUIDE.read_text())["sections"]) - len(waiting)
    print(f"User guide: {detailed} detailed sections; {len(waiting)} still to bring to the bar ({', '.join(waiting)}).")
    return 1 if problems else 0


if __name__ == "__main__":
    sys.exit(main())
