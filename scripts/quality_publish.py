#!/usr/bin/env python3
"""Adds a scheduled quality run's scores to toskar-model-data's results file.

    quality_publish.py <reports folder> <results.json> <version.json> <catalog.json> <run URL>

The reports folder holds a quality-report-real-models-<model> folder per model,
as the Quality workflow uploads them. A model gets a row only when its reports
say they tested that model: "recommended" stands for several, and a report
naming another model means the run didn't test the one it was asked to. Running
this again for the same run adds nothing.
"""

import datetime
import json
import re
import sys
from pathlib import Path

from quality_result import score, seconds_per_question

FOLDER_PREFIX = "quality-report-real-models-"
REPORTS = {"quality": "report.md", "topics": "topics.md", "files": "files.md", "reasoning": "reasoning.md", "triage": "triage.md"}
# A graphics card's driver name lists its whole family.
CARD_NAMES = {"AMD Radeon RX 7900 XT/7900 XTX/7900 GRE/7900M": "AMD RX 7900 XTX"}


def tested_on(text):
    """The model and hardware a report says it ran on, or None."""
    found = re.search(r"^Tested Toskar .*? on (.+?)(?: \(\d+ GB\))?, with (.+?); the model ran on the (GPU \((\w+)|CPU)", text, re.M)
    if not found:
        return None
    return found[2], f"{CARD_NAMES.get(found[1], found[1])}, {found[4] or 'CPU'}"


def row_for(model, folder, suites, name, version, source):
    """A model's row, or None and why not."""
    scores = {}
    hardware = None
    for suite, file in REPORTS.items():
        report = folder / file
        if suite not in suites or not report.exists():
            continue
        text = report.read_text()
        tested = tested_on(text)
        if not tested:
            continue
        if tested[0] != model:
            return None, f"its report says it tested {tested[0]}"
        hardware = hardware or tested[1]
        found = score(suite, text)
        if found:
            scores[suite] = found
    if not scores:
        return None, "no scores in its reports"
    row = {
        "model": model,
        "name": name,
        "date": datetime.datetime.now(datetime.timezone.utc).date().isoformat(),
        "hardware": hardware,
        "method_version": version,
        "source": source,
        "scores": scores,
    }
    reasoning = folder / REPORTS["reasoning"]
    seconds = seconds_per_question(reasoning.read_text()) if reasoning.exists() else None
    if seconds is not None:
        row["seconds_per_question"] = seconds
    return row, None


def main(reports, results_path, version_path, catalog_path, source):
    results_path = Path(results_path)
    text = results_path.read_text()
    results = json.loads(text)
    version = json.loads(Path(version_path).read_text())["version"]
    names = {m["id"]: m.get("display_name", m["id"]) for m in json.loads(Path(catalog_path).read_text())["models"]}
    done = {r["model"] for r in results["results"] if r.get("source") == source}

    rows = []
    for folder in sorted(Path(reports).glob(FOLDER_PREFIX + "*")):
        model = folder.name.removeprefix(FOLDER_PREFIX)
        if model in done:
            continue
        row, why = row_for(model, folder, results["suites"], names.get(model, model), version, source)
        if row:
            rows.append(row)
            print(json.dumps(row, ensure_ascii=False))
        else:
            print(f"No row for {model}: {why}.")
    if rows:
        # Each row stays on its own line, so the file's history shows what was added.
        end = text.rindex("\n  ]")
        added = "".join(",\n    " + json.dumps(row, ensure_ascii=False) for row in rows)
        results_path.write_text(text[:end] + added + text[end:])
    return 0


if __name__ == "__main__":
    sys.exit(main(*sys.argv[1:6]))
