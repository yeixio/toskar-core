# Live voice mode

You talk, Toskar answers out loud, and you can talk over it (#795). Everything
stays on the computer. This page is the protocol and how it works; the issue
has the research behind it.

## The connection

`GET /api/v1/voice/realtime` upgrades to a WebSocket (subprotocol `realtime`).
Each message is one JSON event, as in OpenAI's Realtime API, so a client
written for that API needs only the URL changed. Sign in as for the rest of
the API: a key in `Authorization`, the web app's session, or a paired
device's key. The caller needs the voice capability.

Audio both ways is `pcm16`: 16-bit little-endian PCM, mono, 24 kHz, base64.

## Events

From the client:

| Event | What it does |
| --- | --- |
| `session.update` | Sets `voice` (a Piper voice), `turn_detection` (`server_vad` with `threshold` 0 to 1, `prefix_padding_ms`, `silence_duration_ms`, `create_response`, `interrupt_response`; `null` turns it off), `input_audio_transcription.language` (a hint such as `de`), and `modalities` (`["text"]` for text only). Only `pcm16` audio is taken. |
| `input_audio_buffer.append` | `audio`: the next piece of the microphone's audio. Send about 100 ms at a time. |
| `input_audio_buffer.commit` | Ends a turn, when turn detection is off. |
| `input_audio_buffer.clear` | Drops the turn so far. |
| `response.create` | Answers the last turn, when turns don't answer themselves. |
| `response.cancel` | Stops the answer. |
| `conversation.item.truncate` | Says how much of an answer was heard, after the person cut it off. |

From the server, in order for one turn:

1. `input_audio_buffer.speech_started`, then `speech_stopped`, with `audio_start_ms` and `audio_end_ms`.
2. `input_audio_buffer.committed`, `conversation.item.created`, and `conversation.item.input_audio_transcription.completed` with `transcript`.
3. `response.created`, `response.output_item.added`, then for each sentence `response.audio_transcript.delta` followed by its `response.audio.delta` pieces (100 ms each).
4. `response.audio.done`, `response.audio_transcript.done` with the whole `transcript`, `response.output_item.done`, and `response.done`.

`response.done`'s `response.status` is `completed`, `cancelled` (the person
talked over it, or `response.cancel`), or `failed`, with `status_details`.
Its `metadata.toskar_latency` says how long each stage took, in milliseconds:
`stt_ms` (from the end of speech to the transcript), `llm_first_token_ms`,
`first_audio_ms` (both from the start of the answer), and `end_to_end_ms`,
from the end of speech to the first audio, which is what the person waits.

Errors are `error` events with `error.code`, such as `invalid_audio`,
`unsupported_audio_format`, `input_audio_buffer_commit_empty`, and
`speech_unavailable`.

## How a turn is answered

- **Turn detection** (`internal/voice/vad.go`) compares each 20 ms frame's
  loudness with the room's own noise. 60 ms of speech starts a turn, so a
  click doesn't, and `silence_duration_ms` of quiet ends it. While Toskar
  answers, speech starts a new turn and stops the answer (barge-in).
- **Listening** is Whisper (faster-whisper), as dictation uses.
- **Thinking** goes through the person's normal chat: their default profile,
  memory, and tools. A session is one chat, named Voice chat, in their
  chats. The answer is asked to be short and spoken, with no Markdown, and
  without the model's thinking first.
- **Speaking** is Piper, a sentence at a time, so the first sentence plays
  while the rest is written. Markdown marks and links' addresses aren't read
  out.

## Not yet

- Whisper and Piper start for each turn; a speech worker that keeps them
  loaded comes next, then the measurements on the Mac, the rig, and a
  CPU-only computer.
- A model that judges whether a turn sounds finished (Smart Turn), and
  more natural voices (Kokoro, Qwen3-TTS).
- The chat keeps the whole answer even when it was cut off.
- `/v1/realtime` and the key in the WebSocket subprotocol, for OpenAI's
  SDKs; the web app, the phone, the TV, and the doorbell (#667).
