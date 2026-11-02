#!/usr/bin/env python3
"""Writes result.json from a run's reports, for scripts/quality-score.sh.

    quality_result.py <model> <reports folder> <version.json> <hardware JSON>

It reads each report's own sentence for its score, and leaves out a set whose
report says nothing it can read, rather than guess.
"""

import datetime
import json
import re
import sys
from pathlib import Path


SCORE_SENTENCES = {
    "reasoning": r"(\d+) of (\d+) right",
    "triage": r"type right on (\d+) of (\d+)",
}


def score(suite, text):
    """A set's passed and of, from its report's summary sentence."""
    found = re.search(SCORE_SENTENCES.get(suite, r"(\d+) of (\d+) (?:cases|runs) passed"), text)
    return {"passed": int(found[1]), "of": int(found[2])} if found else None


def seconds_per_question(text):
    """The average seconds a reasoning question took, from its report."""
    found = re.search(r"right \(\d+%\), ([\d.]+) s", text)
    return float(found[1]) if found else None


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
    reasoning = out / "reasoning.md"
    seconds = seconds_per_question(reasoning.read_text()) if reasoning.exists() else None
    if seconds is not None:
        row["seconds_per_question"] = seconds
    (out / "result.json").write_text(json.dumps(row, indent=2) + "\n")
    print(json.dumps(row, indent=2))
    print(f"\nSaved in {out}. To share it: https://github.com/yeixio/toskar-core/issues/new?template=quality_results.yml")
    return 0 if scores else 1


if __name__ == "__main__":
    sys.exit(main(*sys.argv[1:5]))
