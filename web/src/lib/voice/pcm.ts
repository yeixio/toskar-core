/** The rate live voice sends and receives audio at: OpenAI Realtime's pcm16. */
export const VOICE_RATE = 24_000

/**
 * Resamples mono audio linearly from one rate to another: enough for
 * speech, such as a microphone's 48 kHz to 24 kHz.
 */
export function resample(samples: Float32Array, from: number, to: number): Float32Array {
  if (from === to || samples.length === 0) return samples
  const n = Math.floor((samples.length * to) / from)
  const out = new Float32Array(n)
  const step = from / to
  for (let i = 0; i < n; i++) {
    const pos = i * step
    const j = Math.floor(pos)
    const frac = pos - j
    const a = samples[j] ?? 0
    const b = samples[j + 1] ?? a
    out[i] = a + (b - a) * frac
  }
  return out
}

/** 16-bit little-endian PCM from samples between -1 and 1. */
export function toPCM16(samples: Float32Array): Uint8Array {
  const out = new Uint8Array(samples.length * 2)
  const view = new DataView(out.buffer)
  for (let i = 0; i < samples.length; i++) {
    const s = Math.max(-1, Math.min(1, samples[i]))
    view.setInt16(i * 2, s < 0 ? s * 0x8000 : s * 0x7fff, true)
  }
  return out
}

/** Samples between -1 and 1 from 16-bit little-endian PCM. */
export function fromPCM16(bytes: Uint8Array): Float32Array {
  const n = Math.floor(bytes.length / 2)
  const out = new Float32Array(n)
  const view = new DataView(bytes.buffer, bytes.byteOffset, n * 2)
  for (let i = 0; i < n; i++) out[i] = view.getInt16(i * 2, true) / 0x8000
  return out
}

export function toBase64(bytes: Uint8Array): string {
  let s = ''
  const chunk = 0x8000
  for (let i = 0; i < bytes.length; i += chunk) {
    s += String.fromCharCode(...bytes.subarray(i, i + chunk))
  }
  return btoa(s)
}

export function fromBase64(text: string): Uint8Array {
  const s = atob(text)
  const out = new Uint8Array(s.length)
  for (let i = 0; i < s.length; i++) out[i] = s.charCodeAt(i)
  return out
}
