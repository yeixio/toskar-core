"""Live voice mode's speech worker (toskar-core#795): Whisper and Piper kept
loaded between turns, so a turn doesn't wait for Python and the models to
start.

Reads one JSON request per line on stdin and writes one JSON answer per line
on stdout, with the request's id:
  {"id", "op": "transcribe", "pcm", "model", "models_dir", "language"}
      pcm is base64 16-bit mono PCM at 16 kHz; answers {"id", "text", "language",
      "probability"}: how sure Whisper is of the language, from 0 to 1.
  {"id", "op": "speak", "text", "voice", "voices_dir"}
      answers {"id", "pcm", "rate"}: base64 16-bit mono PCM at the voice's rate.
  {"id", "op": "speak", "engine": "kokoro", "text", "voice", "lang", "model", "voices"}
      reads in a Kokoro voice, such as af_heart, from the downloaded model
      and voices files; lang is espeak's, such as en-us. Answers as above,
      at 24 kHz.
  {"id", "op": "speak", "engine": "kitten", "text", "voice", "lang", "model"}
      reads in a Kitten TTS 2 mini voice, such as Eleanor, from the model
      folder (toskar-core#1304); runs in Kitten's own environment. Answers
      as above, at 24 kHz.
  {"id", "op": "fetch", "repo", "revision", "dir", "patterns"}
      downloads a Hugging Face repository's files at a revision into dir.
  {"id", "op": "turn", "pcm", "model"}
      whether the person has finished speaking (toskar-core#1403): Smart
      Turn v3 on the last 8 s of the turn, pcm as for transcribe; model is
      its ONNX file. Answers {"id", "finished"}: the probability, 0 to 1.
  {"id", "op": "ping"} answers {"id"}.
Any failure answers {"id", "error"}; the worker goes on.
"""
import base64
import io
import json
import os
import subprocess
import sys
import wave

whisper = {}
turns = {}
voices = {}
kokoro = {}
kitten = {}


def transcribe(req):
    import numpy as np
    from faster_whisper import WhisperModel

    name = req.get("model") or "base"
    model = whisper.get(name)
    if model is None:
        model = WhisperModel(name, device="cpu", compute_type="int8", download_root=req["models_dir"])
        whisper[name] = model
    pcm = np.frombuffer(base64.b64decode(req["pcm"]), dtype=np.int16).astype(np.float32) / 32768.0
    # The turn was already cut at the person's pauses; one beam is enough
    # for a sentence or two, and much faster.
    segments, info = model.transcribe(pcm, language=req.get("language") or None, beam_size=1, vad_filter=False,
                                      condition_on_previous_text=False)
    # Whisper writes words even for noise, such as "Thank you." for a fan:
    # a segment it rates as probably not speech, and isn't sure of, is left
    # out, by Whisper's own rule (#795).
    text = " ".join(s.text.strip() for s in segments
                    if not (s.no_speech_prob > 0.6 and s.avg_logprob < -1.0)).strip()
    return {"text": text, "language": info.language, "probability": float(info.language_probability or 0)}


def voice_for(req):
    from piper import PiperVoice
    from piper.phonemize_espeak import ESPEAK_DATA_DIR

    name = req["voice"]
    v = voices.get(name)
    if v is not None:
        return v
    model = os.path.join(req["voices_dir"], name + ".onnx")
    if not os.path.exists(model):
        subprocess.run([sys.executable, "-m", "piper.download_voices", "--data-dir", req["voices_dir"], name],
                       check=True, stdout=sys.stderr)
    # espeak-ng ignores a data path longer than about 160 characters and
    # looks where Piper was built instead, so give it a relative one.
    os.chdir(os.path.dirname(ESPEAK_DATA_DIR))
    v = PiperVoice.load(model, espeak_data_dir=os.path.basename(ESPEAK_DATA_DIR), download_dir=req["voices_dir"])
    voices[name] = v
    return v


def short_espeak():
    """espeak-ng ignores a data path longer than about 160 characters (see
    voice_for), so Kokoro's goes through a link with a short one. Piper's
    relies on the working folder, which this leaves as it is."""
    import tempfile

    import espeakng_loader
    from kokoro_onnx.config import EspeakConfig

    data = espeakng_loader.get_data_path()
    if len(data) > 120:
        link = os.path.join(tempfile.mkdtemp(prefix="tk"), "d")
        try:
            os.symlink(data, link, target_is_directory=True)
            data = link
        except OSError:
            pass  # Windows without the right to link: the long path it is
    return EspeakConfig(data_path=data, lib_path=espeakng_loader.get_library_path())


def speak_kokoro(req):
    import numpy as np
    from kokoro_onnx import Kokoro

    k = kokoro.get(req["model"])
    if k is None:
        k = Kokoro(req["model"], req["voices"], espeak_config=short_espeak())
        kokoro[req["model"]] = k
    samples, rate = k.create(req["text"], voice=req["voice"], speed=float(req.get("speed", 1.0)), lang=req["lang"])
    pcm = (np.clip(samples, -1.0, 1.0) * 32767).astype("<i2").tobytes()
    return {"pcm": base64.b64encode(pcm).decode("ascii"), "rate": rate}


def speak_kitten(req):
    import numpy as np
    from kittenml.kittentts2_tiny import KittenTTS2Tiny

    m = kitten.get(req["model"])
    if m is None:
        m = KittenTTS2Tiny(req["model"])
        kitten[req["model"]] = m
    samples = m.generate(req["text"], voice=req["voice"], language=req["lang"], speed=float(req.get("speed", 1.0)))
    pcm = (np.clip(samples, -1.0, 1.0) * 32767).astype("<i2").tobytes()
    return {"pcm": base64.b64encode(pcm).decode("ascii"), "rate": m.sample_rate}


def turn(req):
    """Smart Turn v3 (BSD-2-Clause, pipecat-ai/smart-turn): a Whisper Tiny
    encoder that hears from the audio whether a sentence sounds finished.
    Its features are Whisper's log-mel over 8 s, from the end of the turn,
    padded with silence before it, as its reference inference does."""
    import numpy as np
    import onnxruntime as ort
    from faster_whisper.feature_extractor import FeatureExtractor

    path = req["model"]
    session = turns.get(path)
    if session is None:
        so = ort.SessionOptions()
        so.execution_mode = ort.ExecutionMode.ORT_SEQUENTIAL
        so.inter_op_num_threads = 1
        so.graph_optimization_level = ort.GraphOptimizationLevel.ORT_ENABLE_ALL
        session = ort.InferenceSession(path, sess_options=so, providers=["CPUExecutionProvider"])
        turns[path] = (session, FeatureExtractor(feature_size=80, chunk_length=8))
        session = turns[path]
    model, features = session
    n = 8 * 16000
    pcm = np.frombuffer(base64.b64decode(req["pcm"]), dtype=np.int16).astype(np.float32) / 32768.0
    pcm = pcm[-n:]
    if len(pcm) < n:
        pcm = np.concatenate([np.zeros(n - len(pcm), dtype=np.float32), pcm])
    # The feature extractor's do_normalize: zero mean and unit variance.
    pcm = (pcm - pcm.mean()) / np.sqrt(pcm.var() + 1e-7)
    mel = features(pcm, padding=0)[:, :800].astype(np.float32)
    if mel.shape[1] < 800:
        mel = np.pad(mel, ((0, 0), (0, 800 - mel.shape[1])))
    out = model.run(None, {"input_features": mel[np.newaxis, :, :]})
    return {"finished": float(np.asarray(out[0]).reshape(-1)[0])}


def fetch(req):
    from huggingface_hub import snapshot_download

    snapshot_download(repo_id=req["repo"], revision=req["revision"], local_dir=req["dir"],
                      allow_patterns=req["patterns"])
    return {}


def speak(req):
    if req.get("engine") == "kokoro":
        return speak_kokoro(req)
    if req.get("engine") == "kitten":
        return speak_kitten(req)
    v = voice_for(req)
    syn = None
    speed = float(req.get("speed", 1.0))
    if speed != 1.0:
        # Piper's length scale is how long each sound lasts: slower is longer.
        from piper import SynthesisConfig

        syn = SynthesisConfig(length_scale=v.config.length_scale / speed)
    buf = io.BytesIO()
    with wave.open(buf, "wb") as wav:
        v.synthesize_wav(req["text"], wav, syn_config=syn)
    buf.seek(0)
    with wave.open(buf, "rb") as wav:
        rate = wav.getframerate()
        frames = wav.readframes(wav.getnframes())
    return {"pcm": base64.b64encode(frames).decode("ascii"), "rate": rate}


def main():
    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue
        req = {}
        try:
            req = json.loads(line)
            op = req.get("op")
            if op == "transcribe":
                out = transcribe(req)
            elif op == "speak":
                out = speak(req)
            elif op == "fetch":
                out = fetch(req)
            elif op == "turn":
                out = turn(req)
            elif op == "ping":
                out = {}
            else:
                raise ValueError("unknown op %r" % op)
        except Exception as e:  # noqa: BLE001 - every failure is answered
            out = {"error": "%s: %s" % (type(e).__name__, e)}
        out["id"] = req.get("id", "")
        sys.stdout.write(json.dumps(out) + "\n")
        sys.stdout.flush()


if __name__ == "__main__":
    main()
