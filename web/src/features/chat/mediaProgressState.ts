import type { TFunction } from 'i18next'

/** What's being made: a picture, a changed picture, or a clip. */
export type MediaKind = 'image' | 'edit' | 'video'

/** How far a picture or clip has got, from core's tool.progress (#706). */
export type MediaProgressState = {
  stage: 'starting' | 'loading' | 'drawing' | 'finishing'
  step: number
  steps: number
  secondsLeft: number
}

/** The kind of media a tool makes, or null for any other tool. */
export function mediaKindOf(toolId?: string): MediaKind | null {
  if (toolId === 'image.generate') return 'image'
  if (toolId === 'image.edit') return 'edit'
  if (toolId === 'video.generate') return 'video'
  return null
}

/** Core's tool.progress payload as state; null when it isn't one. */
export function mediaProgressFrom(payload: Record<string, unknown> | undefined): MediaProgressState | null {
  const stage = payload?.stage
  if (stage !== 'loading' && stage !== 'drawing' && stage !== 'finishing') return null
  return {
    stage,
    step: Number(payload?.step) || 0,
    steps: Number(payload?.steps) || 0,
    secondsLeft: Number(payload?.seconds_left) || 0,
  }
}

/** The lines about what Ratatoskr is up to while one is made (#706), in the App language. */
export function mediaLines(t: TFunction, kind: MediaKind): string[] {
  const lines = t(kind === 'video' ? 'mediaProgress.ratatoskr.video' : 'mediaProgress.ratatoskr.image', { returnObjects: true })
  return lines && typeof lines === 'object' ? Object.values(lines as Record<string, string>).filter((l) => typeof l === 'string') : []
}
