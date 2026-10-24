"""Connect as a device of its own, as the Toskar app does: pair once with
the code Connect a device shows, then reach the computer at home or, with
access from anywhere, away from home. Opening the computer's address
record away from home needs the anywhere extra:

    pip install "toskar[anywhere]"
    python pair.py 192.168.1.20:7331 123456   # once, at home
    python pair.py                            # then, from anywhere
"""

import json
import sys
from pathlib import Path

from toskar import Device, NotConfirmed, Toskar, pair

SAVED = Path("device.json")  # Keep it as a password is kept: it holds the device's key.

if len(sys.argv) == 3:
    address, code = sys.argv[1:]

    def confirm(short_pin: str) -> bool:
        # The computer shows the same short pin next to the code.
        return input(f"Does the computer show {short_pin}? [y/N] ").strip().lower() == "y"

    try:
        device = pair(address, code, "Python example", confirm)
    except NotConfirmed:
        sys.exit("Not paired: the short pin wasn't confirmed.")
    SAVED.write_text(json.dumps(device.to_dict()))
    SAVED.chmod(0o600)
    print("Paired. It shows in Devices on that computer.")

device = Device.from_dict(json.loads(SAVED.read_text()))
with Toskar(device=device) as toskar:
    version = toskar.version()
    # home, direct, or relay: the way in it found.
    print(f"Reached Toskar {version.version} {toskar.path}.")
