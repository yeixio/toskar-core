import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { MascotState } from '@/lib/ratatoskr/rig'
import { ChatActivity } from './ChatActivity'
import { mediaLines, type MediaKind, type MediaProgressState } from './mediaProgressState'

/** After this long, a line about what Ratatoskr is up to joins the facts (#706). */
const FUN_AFTER_MS = 8_000
/** How long each of those lines stays. */
const FUN_EVERY_MS = 6_000

/**
 * While a picture or clip is made (#706): the step and an estimate on the
 * activity line, a frame the shape of what's coming with how far it has
 * got, and, on a long wait, what Ratatoskr is up to. The facts come first;
 * screen readers hear them, not the jokes.
 */
export function MediaProgress({
  kind,
  progress,
  label,
  mascot,
}: {
  kind: MediaKind
  progress: MediaProgressState
  label: string
  mascot?: MascotState
}) {
  const { t } = useTranslation('chat')
  const lines = mediaLines(t, kind)
  const fun = useRotatingLine(lines.length)
  const fraction = progress.stage === 'finishing' ? 1 : progress.steps > 0 ? progress.step / progress.steps : 0
  const facts = [label]
  if (progress.stage === 'drawing' && progress.steps > 0) facts.push(t('mediaProgress.step', { step: progress.step, steps: progress.steps }))
  if (progress.stage === 'drawing' && progress.secondsLeft > 0) {
    facts.push(
      progress.secondsLeft >= 90
        ? t('mediaProgress.leftMinutes', { minutes: Math.round(progress.secondsLeft / 60) })
        : t('mediaProgress.leftSeconds', { seconds: progress.secondsLeft }),
    )
  }
  return (
    <div className="space-y-2">
      <ChatActivity label={facts.join(' · ')} mascot={mascot} timing={`media-${kind}`} />
      <div
        role="img"
        aria-label={kind === 'video' ? t('mediaProgress.frameVideo') : t('mediaProgress.frameImage')}
        className={[
          'relative ms-11 w-full max-w-xs overflow-hidden rounded-2xl border border-line bg-raised/60',
          kind === 'video' ? 'aspect-video' : 'aspect-square',
        ].join(' ')}
      >
        <div className="absolute inset-0 bg-gradient-to-br from-primary/10 via-transparent to-primary/5 motion-safe:animate-pulse" />
        <div className="absolute inset-x-4 bottom-4 h-1 overflow-hidden rounded-full bg-line/60">
          <div
            className="h-full rounded-full bg-primary transition-[width] duration-700 ease-out"
            style={{ width: `${Math.round(Math.max(0.04, fraction) * 100)}%` }}
            data-testid="media-progress-bar"
          />
        </div>
      </div>
      {fun !== null && lines.length > 0 ? (
        <p className="ms-11 text-xs italic text-ink-faint" aria-hidden="true" data-testid="media-progress-fun">
          {lines[fun % lines.length]}
        </p>
      ) : null}
    </div>
  )
}

/** Which line to show, once the wait has lasted FUN_AFTER_MS; it moves on every FUN_EVERY_MS, unless motion is reduced. */
function useRotatingLine(count: number): number | null {
  const [since] = useState(() => Date.now())
  const [now, setNow] = useState(since)
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(id)
  }, [])
  const elapsed = now - since
  if (count === 0 || elapsed < FUN_AFTER_MS) return null
  const still = typeof window !== 'undefined' && window.matchMedia?.('(prefers-reduced-motion: reduce)').matches
  // A different first line each time, by when the wait began.
  const start = Math.floor(since / 1000) % count
  return still ? start : start + Math.floor((elapsed - FUN_AFTER_MS) / FUN_EVERY_MS)
}
