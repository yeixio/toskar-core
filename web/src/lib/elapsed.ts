/** After this long on one step, such as making a picture, the time so far shows beside it (#705). */
export const ELAPSED_AFTER_MS = 15_000

/** Minutes and seconds, such as 1:20, or 1:02:05 past an hour. */
export function formatElapsed(ms: number): string {
  const total = Math.max(0, Math.floor(ms / 1000))
  const h = Math.floor(total / 3600)
  const m = Math.floor((total % 3600) / 60)
  const s = String(total % 60).padStart(2, '0')
  return h > 0 ? `${h}:${String(m).padStart(2, '0')}:${s}` : `${m}:${s}`
}
