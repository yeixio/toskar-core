#!/usr/bin/env python3
"""Writes result.json from a run's reports, for scripts/quality-score.sh.

    quality-result.py <model> <reports folder> <version.json> <hardware JSON>

It reads each report's own sentence for its score, and leaves out a set whose
report says nothing it can read, rather than guess.
"""

import datetime
import json
import re
import sys
from pathlib import Path


def score(suite, text):
    """A set's passed and of, from its report's summary sentence."""
    if suite == "topics":
        held = re.search(r"held (\d+) of (\d+) off-topic", text)
        refused = re.search(r"wrongly refused (\d+) of (\d+) on-topic", text)
        if not held or not refused:
            return None
        return {"passed": int(held[1]) + int(refused[2]) - int(refused[1]), "of": int(held[2]) + int(refused[2])}
    found = re.search(r"(\d+) of (\d+) right" if suite == "reasoning" else r"(\d+) of (\d+) (?:cases|runs) passed", text)
    return {"passed": int(found[1]), "of": int(found[2])} if found else None


def hardware_name(hw):
    """The graphics card or chip, and memory. Nothing that identifies the computer."""
    accelerators = [a.get("name") or a.get("model") for a in hw.get("accelerators") or []]
    accelerators = [a for a in accelerators if a]
    chip = accelerators[0] if accelerators else (hw.get("cpu") or {}).get("model", "")
    memory = round((hw.get("memory") or {}).get("total_bytes", 0) / 2**30)
    return f"{chip}, {memory} GB"


def main(model, out, version, hardware):
    out = Path(out)
    scores = {}
    for suite in ("quality", "topics", "files", "reasoning"):
        report = out / f"{suite}.md"
        found = score(suite, report.read_text()) if report.exists() else None
        if found:
            scores[suite] = found
        else:
            print(f"No score from the {suite} set: see {report}", file=sys.stderr)
    row = {
        "model": model,
        "name": model,
        "date": datetime.date.today().isoformat(),
        "hardware": hardware_name(json.loads(hardware)),
        "method_version": json.loads(Path(version).read_text())["version"],
        "scores": scores,
    }
    (out / "result.json").write_text(json.dumps(row, indent=2) + "\n")
    print(json.dumps(row, indent=2))
    print(f"\nSaved in {out}. To share it: https://github.com/yeixio/toskar-core/issues/new?template=quality_results.yml")
    return 0 if scores else 1


if __name__ == "__main__":
    sys.exit(main(*sys.argv[1:5]))
