"""Stream an answer, a piece at a time, with the Python SDK.

    python chat.py "Plan a three-day trip to Juneau"
"""

import sys

from toskar import ErrorCodes, Toskar, ToskarError

message = " ".join(sys.argv[1:]) or "Hello"

# This computer's Toskar Core needs no key. Elsewhere, set TOSKAR_URL and
# TOSKAR_API_KEY, or pass base_url and api_key.
with Toskar() as toskar:
    try:
        for event in toskar.chat_stream(message):
            if event.type == "token":
                print(event.content, end="", flush=True)
            elif event.type == "done" and event.metrics:
                print(f"\n\n{event.metrics.completion_tokens} tokens")
    except ToskarError as err:
        if err.code == ErrorCodes.NO_MODEL_INSTALLED:
            sys.exit("Install a model first: open Toskar and choose one in Models.")
        raise
