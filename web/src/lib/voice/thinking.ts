/**
 * The thinking sound (#1334), the same chime Core plays at the door: two
 * warm notes a fifth apart, each a quick soft start and a bell's fade,
 * about 0.6 s, well below speech. Made here rather than shipped as a
 * recording, so it plays at any rate as it is.
 */
export const THINKING_DELAY_MS = 1500
export const THINKING_GAP_MS = 2500
const PEAK = 0.12

export function thinkingSound(rate: number): Float32Array {
  if (rate <= 0) return new Float32Array(0)
  const notes = [
    { at: 0, freq: 659.25 }, // E5
    { at: 0.16, freq: 987.77 }, // B5
  ]
  const n = Math.floor(0.62 * rate)
  const x = new Float32Array(n)
  for (const note of notes) {
    const start = Math.floor(note.at * rate)
    for (let i = start; i < n; i++) {
      const t = (i - start) / rate
      const env = Math.min(1, t / 0.012) * Math.exp(-t / 0.11)
      const w = 2 * Math.PI * note.freq * t
      x[i] += env * (Math.sin(w) + 0.18 * Math.sin(2 * w))
    }
  }
  let peak = 0
  for (const v of x) peak = Math.max(peak, Math.abs(v))
  // The last 20 ms fade to silence, so it ends without a click.
  const tail = Math.floor(0.02 * rate)
  for (let i = 0; i < n; i++) {
    const left = n - i
    x[i] = (x[i] / peak) * PEAK * (left < tail ? left / tail : 1)
  }
  return x
}
