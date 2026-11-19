"""Makes the voice bench's speech clips (tests/voice) with eSpeak NG, so
anyone can make them again and none are recordings of a person.

    python -m venv venv && venv/bin/pip install espeakng-loader
    venv/bin/python -I tests/voice/clips/make.py tests/voice/clips

Reads clips.json beside this file and writes each clip's WAV: 16-bit mono
PCM at 16 kHz, trimmed to the speech.
"""
import array
import ctypes
import json
import os
import sys
import wave

import espeakng_loader

OUT_RATE = 16000


def main(folder):
    lib = ctypes.CDLL(espeakng_loader.get_library_path())
    rate = lib.espeak_Initialize(1, 500, espeakng_loader.get_data_path().encode(), 0)  # AUDIO_OUTPUT_RETRIEVAL
    buf = []
    callback_type = ctypes.CFUNCTYPE(ctypes.c_int, ctypes.POINTER(ctypes.c_short), ctypes.c_int, ctypes.c_void_p)

    @callback_type
    def collect(wav, n, _events):
        if wav and n > 0:
            buf.extend(wav[:n])
        return 0

    lib.espeak_SetSynthCallback(collect)
    with open(os.path.join(folder, "clips.json"), encoding="utf-8") as f:
        clips = json.load(f)["clips"]
    for clip in clips:
        buf.clear()
        lib.espeak_SetVoiceByName(clip["voice"].encode())
        lib.espeak_SetParameter(1, clip.get("speed", 160), 0)  # espeakRATE
        lib.espeak_SetParameter(3, clip.get("pitch", 50), 0)  # espeakPITCH
        text = clip["text"].encode("utf-8")
        lib.espeak_Synth(text, len(text) + 1, 0, 0, 0, 1, None, None)  # espeakCHARS_UTF8
        lib.espeak_Synchronize()
        samples = trim(resample(buf, rate, OUT_RATE))
        with wave.open(os.path.join(folder, clip["id"] + ".wav"), "wb") as w:
            w.setnchannels(1)
            w.setsampwidth(2)
            w.setframerate(OUT_RATE)
            w.writeframes(array.array("h", samples).tobytes())
        print(clip["id"], round(len(samples) / OUT_RATE, 2), "s")


def resample(samples, src, dst):
    n = len(samples) * dst // src
    out = []
    for i in range(n):
        x = i * src / dst
        j = int(x)
        f = x - j
        a = samples[j]
        b = samples[min(j + 1, len(samples) - 1)]
        out.append(int(round(a * (1 - f) + b * f)))
    return out


def trim(samples, threshold=60):
    first = next((i for i, s in enumerate(samples) if abs(s) > threshold), 0)
    last = next((i for i in range(len(samples) - 1, -1, -1) if abs(samples[i]) > threshold), len(samples) - 1)
    return samples[first:last + 1]


if __name__ == "__main__":
    main(sys.argv[1] if len(sys.argv) > 1 else os.path.dirname(os.path.abspath(__file__)))
