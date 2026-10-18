import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Ratatoskr } from '@/components/ui/Ratatoskr'
import type { MascotState } from '@/lib/ratatoskr/rig'
import { ELAPSED_AFTER_MS, formatElapsed } from '@/lib/elapsed'

/** Shown while a reply has not produced visible text yet. Ratatoskr thinks, or delivers when the reply runs on a paired computer. */
export function ChatActivity({ label, mascot = 'think' }: { label?: string | null; mascot?: MascotState }) {
  const { t } = useTranslation('chat')
  const elapsed = useElapsed(label ?? '')
  return (
    <div
      className="flex max-w-[min(42rem,85%)] items-center gap-3 rounded-2xl bg-raised/80 px-4 py-3"
      role="status"
      aria-live="polite"
      aria-label={label || t('status.workingLabel')}
    >
      <Ratatoskr state={mascot} size={32} />
      <span className="text-sm text-ink-muted">{label || t('status.working')}</span>
      {/* Hidden from screen readers, which would otherwise hear it every second. */}
      {elapsed >= ELAPSED_AFTER_MS ? (
        <span className="text-sm tabular-nums text-ink-muted/80" aria-hidden="true" data-testid="activity-elapsed">
          {formatElapsed(elapsed)}
        </span>
      ) : null}
    </div>
  )
}

/** How long the current step has shown, counted again when it changes. */
function useElapsed(step: string): number {
  const [since, setSince] = useState(() => Date.now())
  const [now, setNow] = useState(since)
  useEffect(() => {
    const start = Date.now()
    setSince(start)
    setNow(start)
    const id = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(id)
  }, [step])
  return now - since
}
