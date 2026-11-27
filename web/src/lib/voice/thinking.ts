/**
 * The thinking sound (#1334), the same one Core plays at the door: four
 * soft rising bubbles, half a second in all, well below speech. Mike chose
 * them on 2026-11-27, after the two-note chime proved too bright. Made here
 * rather than shipped as a recording, so it plays at any rate as it is.
 */
export const THINKING_DELAY_MS = 1500
export const THINKING_GAP_MS = 3000
const PEAK = 0.045

// When each bubble starts, the pitch it rises from and to, how long it
// lasts, and how loud it is beside the others (internal/voice/thinking.go).
const BUBBLES = [
  { at: 0, from: 420, to: 760, length: 0.09, gain: 0.6 },
  { at: 0.12, from: 520, to: 940, length: 0.08, gain: 0.5 },
  { at: 0.25, from: 470, to: 850, length: 0.09, gain: 0.55 },
  { at: 0.37, from: 600, to: 1050, length: 0.07, gain: 0.4 },
]

export function thinkingSound(rate: number): Float32Array {
  if (rate <= 0) return new Float32Array(0)
  const n = Math.floor(0.5 * rate)
  const x = new Float32Array(n)
  for (const b of BUBBLES) {
    const start = Math.floor(b.at * rate)
    const length = Math.floor(b.length * rate)
    let phase = 0
    for (let i = 0; i < length && start + i < n; i++) {
      const t = i / rate
      // It rises in pitch, as a bubble does, and fades quickly.
      phase += (2 * Math.PI * b.from * Math.pow(b.to / b.from, t / b.length)) / rate
      const env = Math.min(1, t / 0.004) * Math.exp(-t / (b.length * 0.35))
      x[start + i] += b.gain * env * Math.sin(phase)
    }
  }
  let peak = 0
  for (const v of x) peak = Math.max(peak, Math.abs(v))
  // The last 10 ms fade to silence, so it ends without a click.
  const tail = Math.floor(0.01 * rate)
  for (let i = 0; i < n; i++) {
    const left = n - 1 - i
    x[i] = (x[i] / peak) * PEAK * (left < tail ? left / tail : 1)
  }
  return x
}
