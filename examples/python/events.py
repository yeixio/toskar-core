"""Print what happens on Toskar as it happens: models loading, answers
being written, automations running. Stop with Ctrl-C.

    python events.py
"""

from toskar import Toskar

with Toskar() as toskar:
    try:
        for event in toskar.events():
            print(event.timestamp, event.type, event.payload, flush=True)
    except KeyboardInterrupt:
        pass
