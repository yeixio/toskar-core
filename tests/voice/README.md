# Voice quality bench

Live voice mode (#795, [docs/voice.md](../../docs/voice.md)) measured the way a
person hears it, so a change to turn detection, speech to text, or the
voices can be scored instead of judged by ear.

The same conversations are played to Toskar in five conditions, each with
the same twelve turns: short questions, a one-word "Yes.", English, Spanish,
German, and French, and turns with a pause in the middle (0.5, 0.7, 0.8,
1.0 and 1.4 s) where someone stops between clauses or to think. Every pause
is mid-sentence, so a good listener waits through it. A sixth scene has
nobody talking: a knock, a cough, a fan switched on, hiss starting, and a
TV starting.

| Condition | What it is |
| --- | --- |
| `headset` | a headset microphone in a quiet room, speech at −20 dBFS |
| `webcam` | a webcam across a room: the room's echo (0.5 s RT60), a small microphone's band (150 Hz–6 kHz), ordinary room sound about 20 dB under the voice, after the browser's automatic gain (−28 dBFS) |
| `hiss` | steady white noise 20 dB under the voice |
| `fan` | a fan or air conditioner's rumble and hum 16 dB under the voice |
| `tv` | other voices (the clips played backwards, so they have no words) 14 dB under the voice |
| `noise` | nobody talks; nothing should start a turn |

The noise is made from a fixed seed, so every run hears the same audio and
a change in score is a change in Toskar.

## Two runs

**`TestTurnTaking`** (`go test ./tests/voice`, in CI) plays the
conversations through Toskar's own turn detection, in this process, with no
models. For each turn it reports:

| Outcome | Meaning |
| --- | --- |
| Heard whole | one turn, with every word kept: the score |
| Answered mid-sentence | the turn ended at a pause, and Toskar would have answered half a sentence |
| Last words cut off | 120 ms or more of speech after the last word kept was left out, so speech to text never hears it |
| First words cut off | the same, before the first word kept |
| Missed | no turn started |
| Never ended | the turn didn't end while the noise went on |

and for each condition the turns started by noise, the speech left out of
turns, and the median wait from the end of speech to the turn ending.

It fails when a condition gets worse than `baseline.json`: fewer turns heard
whole, or more turns from noise. A change that does better keeps its scores
with:

    go test ./tests/voice -run TestTurnTaking -update

and commits `baseline.json` with it. `TOSKAR_VOICE_REPORT=report.md` writes
the table to a file.

**`TestLive`** plays the same conversations, in real time, to a running
Toskar's live voice, with its real Whisper, chat model, and voices. It adds:

| Score | Meaning |
| --- | --- |
| Word errors | word error rate of the transcripts against what was said |
| Words from noise | words transcribed from turns nobody spoke, such as "Thank you." for a fan |
| First audio | median time from the end of speech to the first audio of the answer: what the person waits |
| Wrong language | answers not in the language spoken to it |
| Voice changed | answers whose sentences' pitch differ by 25% or more, as a switch of voice does |

Run it against a test copy, never the Toskar you use (its chats get the
answers), with speech set up:

    TOSKAR_VOICE_URL=ws://127.0.0.1:7400/api/v1/voice/realtime \
    TOSKAR_VOICE_KEY=<key> TOSKAR_VOICE_REPORT=voice.md \
    go test ./tests/voice -run TestLive -v -timeout 30m

It takes about eight minutes. It only reports; models and computers differ
too much for a fixed bar.

## The clips

`clips/` holds the spoken phrases, made with eSpeak NG by
`clips/make.py` from `clips/clips.json`, so none are recordings of a person
and anyone can make them again. eSpeak is clearer and more evenly paced than
a person, with true silence between some words, so it's a fair test of turn
detection's timing but flatters speech to text. Recordings of real voices
(16-bit mono WAV, any rate, trimmed to the speech) can be added to
`clips.json` the same way.
