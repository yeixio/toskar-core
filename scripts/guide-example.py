#!/usr/bin/env python3
"""Print one of the user guide's examples by its test id, for a test that
runs it as written (docs/user-guide/STYLE.md, #885): an install example runs
in the test that installs, on a fresh computer, not in examples/test.py.

    python3 scripts/guide-example.py install-apt
"""

import json
import sys
from pathlib import Path

GUIDE = Path(__file__).resolve().parent.parent / "docs" / "user-guide" / "guide.json"


def example(test_id: str) -> str:
    for section in json.loads(GUIDE.read_text())["sections"]:
        for block in section.get("blocks", []):
            if block.get("type") == "code" and block.get("test") == test_id:
                return block["code"]
    raise SystemExit(f"The user guide has no example {test_id!r}")


if __name__ == "__main__":
    if len(sys.argv) != 2:
        raise SystemExit(__doc__)
    print(example(sys.argv[1]))
