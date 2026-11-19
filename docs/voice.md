# Live voice mode

You talk, Toskar answers out loud, and you can talk over it (#795). Everything
stays on the computer. This page is the protocol and how it works; the issue
has the research behind it.

## The connection

`GET /api/v1/voice/realtime` upgrades to a WebSocket (subprotocol `realtime`).
Each message is one JSON event, as in OpenAI's Realtime API, so a client
written for that API needs only the URL changed. Sign in as for the rest of
the API: a key in `Authorization`, the web app's session, or a paired
device's key. A browser can't set headers on a WebSocket, so it may send
the key as a second subprotocol, `openai-insecure-api-key.<key>`, as
OpenAI's browser clients do. The caller needs the voice capability.

The web app's Talk button, beside Send in a chat, opens voice mode
(`web/src/features/chat/VoiceMode.tsx`): the microphone through the
browser's echo cancellation, resampled to 24 kHz in an AudioWorklet, and
the answer played as it arrives and stopped when the person talks over it.

The desktop app shows the same page, from `wails://` (`wails.localhost` on
Windows), where a WebSocket can't go through the shell's proxy. So the
shell's `VoiceSocketURL` binding gives the daemon's own address, such as
`ws://127.0.0.1:7331/api/v1/voice/realtime`, and the page opens it there.
The desktop's origin is allowed, and a request from this computer needs no
key. On a Mac, the shell lets its own pages use the microphone and macOS
asks the person once.

A client that hears the person itself, as the phone, the car and the TV
do with the device's own speech recognition, can still answer in this
voice: `POST /api/v1/speech/say` with `{text, voice, language}` answers a
WAV file of a sentence or a few (at most 1000 characters), read by the same
worker, and keeps nothing. It's plain HTTPS, so it works through the relay.

## Voices

Empty `voice` is the language's voice from the Voice settings (#1304): the
one an Admin chose (`PUT /api/v1/speech/voices/{language}` with `{voice}`),
or automatic: Kokoro where it speaks the language, then Kitten TTS 2 mini
when it's installed, then Piper. A chosen voice that can't read falls back
to the automatic one. `GET /api/v1/speech/voices` lists each language's
voices and Kitten's install; `POST /api/v1/speech/kitten` installs Kitten in
the background and `DELETE` removes it. All three are Admin only.

Kitten TTS 2 mini (`KittenML/kitten-tts-2-mini`, Apache-2.0) runs in its own
Python environment, `speech-kitten`, because kittenml's English
pronunciation needs PyTorch (the CPU build), which the speech environment
doesn't carry; the speech worker starts a second process from it. Its voices
are named `kitten:<language>:<key>`, such as `kitten:en:eleanor`, and read at
24 kHz. Wherever a Kitten voice is used, show "Powered by KittenML". The
larger Kitten TTS 2, under the Stellon Labs Community License, isn't offered
(#1319).

Measured on an M5 Pro (#1304): the environment is 1.3 GB and the model
189 MB (140 MB without the 4-bit model, which isn't downloaded). A warm
sentence takes 0.36 s for about 3 s of English and 0.6 to 0.75 s in other
languages; the first English sentence after the worker starts takes 2 to
3 s while it loads spaCy and the pronunciation model, 20 s the very first
time, which the install does once. The worker peaks near 1 GB. Japanese
needs kittenml's `ja` extra, whose pyopenjtalk has no macOS wheel and builds
with a C++ compiler, so it's left out for now (#1319).

Audio both ways is `pcm16`: 16-bit little-endian PCM, mono, 24 kHz, base64.

## Events

From the client:

| Event | What it does |
| --- | --- |
| `session.update` | Sets `voice` (a Kitten voice such as `kitten:en:eleanor`, a Kokoro voice such as `af_heart`, or a Piper voice; empty is the language's own), `turn_detection` (`server_vad` with `threshold` 0 to 1, `prefix_padding_ms`, `silence_duration_ms`, `create_response`, `interrupt_response`; `null` turns it off), `input_audio_transcription.language` (a hint such as `de`), and `modalities` (`["text"]` for text only). Only `pcm16` audio is taken. |
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
  loudness with the room's own noise. 160 ms of speech starts a turn, so a
  click, a cough, or a knock doesn't, and a dip between syllables doesn't
  start the count over. `silence_duration_ms` of quiet ends it: 900 ms by
  default, so a pause mid-sentence doesn't end a turn and get answered.
  While Toskar answers, speech starts a new turn and stops the answer
  (barge-in).
- **Listening** is Whisper (faster-whisper), as dictation uses. A speech
  worker (`internal/speech/worker.go`) keeps Whisper and the voices loaded
  in one Python process between turns, and stops it after 10 minutes
  without a turn, which frees its memory.
- **Thinking** goes through the person's normal chat: their default profile,
  memory, and tools. A session is one chat, named Voice chat, in their
  chats. The answer is asked to be short and spoken, with no Markdown, and
  without the model's thinking first.
- **Speaking** reads one answer in one language's voice, chosen at its
  first sentence when that's long enough to tell, else from what the person
  said, so a short "Claro." doesn't switch the accent. It is a sentence at
  a time, so the first sentence plays while
  the rest is written. Markdown marks and links' addresses aren't read out.
  The voice is Kokoro (Kokoro-82M, Apache 2.0, through kokoro-onnx, MIT),
  which sounds much closer to a person than Piper and still runs quickly on
  a processor. Its model (120 MB, checked against pinned checksums)
  downloads in the background the first time; until then, and for the
  languages it doesn't speak well, Piper reads. Kokoro's voices by language:

  | Language | Voice |
  | --- | --- |
  | English | `af_heart` (`bf_emma` for en-GB) |
  | Spanish | `ef_dora` |
  | French | `ff_siwis` |
  | Italian | `if_sara` |
  | Portuguese | `pf_dora` |

  Every other language, such as German, Japanese, Korean, and Chinese,
  is Kitten's voice for it when Kitten TTS 2 mini is installed (except
  Japanese, below), and Piper's otherwise. When Kokoro fails, Piper reads for the next 10
  minutes. Like Piper, Kokoro turns text into sounds with espeak-ng
  (GPL-3.0, installed on the person's computer with the speech packages).

## Not yet

- The measurements on the Mac, the rig, and a CPU-only computer.
- A model that judges whether a turn sounds finished (Smart Turn), and
  Qwen3-TTS for computers with a strong graphics card.
- The chat keeps the whole answer even when it was cut off.
- `/v1/realtime`, for OpenAI's SDKs; the phone, the TV, and the doorbell
  (#667).
