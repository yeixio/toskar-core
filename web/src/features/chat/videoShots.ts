import type { TFunction } from 'i18next'

/** A longer video's shots from its arguments, as core reads them: a prompt, or {prompt, seconds} (#790). */
export function shotsFrom(args: Record<string, unknown> | undefined): { prompt: string; seconds: number }[] | undefined {
  const list = args?.shots
  if (!Array.isArray(list)) return undefined
  const shots = list.flatMap((item) => {
    const shot = typeof item === 'string' ? { prompt: item } : item && typeof item === 'object' ? (item as { prompt?: unknown; seconds?: unknown }) : {}
    const prompt = typeof shot.prompt === 'string' ? shot.prompt.trim() : ''
    if (!prompt) return []
    const asked = typeof shot.seconds === 'number' && shot.seconds > 0 ? shot.seconds : 5
    return [{ prompt, seconds: Math.min(Math.max(asked, 1), 5) }]
  })
  return shots.length ? shots : undefined
}

/** A length in minutes as hours and minutes, such as "3 h 15 min". */
export function shotsDuration(t: TFunction, minutes: number): string {
  const h = Math.floor(minutes / 60)
  const m = minutes % 60
  if (h === 0) return t('tools.shots.minutes', { n: m })
  if (m === 0) return t('tools.shots.hours', { n: h })
  return t('tools.shots.hoursMinutes', { hours: h, minutes: m })
}
