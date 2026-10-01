import { useEffect, useId, useRef, useState } from 'react'
import {
  contextRows,
  fillPercent,
  formatTokens,
  type ContextUsage,
} from './contextUsage'

const rowColor: Record<string, string> = {
  instructions: 'bg-ink-faint',
  tools: 'bg-violet-400',
  conversation: 'bg-primary',
  toolResults: 'bg-amber-400',
}

export function ContextUsageButton({
  usage,
  windowLimit,
}: {
  usage: ContextUsage | null
  windowLimit: number
}) {
  const [open, setOpen] = useState(false)
  const rootRef = useRef<HTMLDivElement>(null)
  const titleId = useId()
  const limit = usage?.limit || windowLimit
  const used = usage?.promptTokens ?? 0
  const percent = fillPercent(used, limit)
  const rows = usage ? contextRows(usage) : []
  const full = percent >= 100

  useEffect(() => {
    if (!open) return
    const onPointer = (event: PointerEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) setOpen(false)
    }
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setOpen(false)
    }
    document.addEventListener('pointerdown', onPointer)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('pointerdown', onPointer)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])

  return (
    <div ref={rootRef} className="relative shrink-0">
      <button
        type="button"
        className="inline-flex h-9 w-9 items-center justify-center rounded-full text-ink-muted transition hover:bg-raised hover:text-ink"
        aria-label={usage ? `Context ${percent}% full` : 'Context usage'}
        aria-expanded={open}
        aria-controls={titleId}
        title="Context usage"
        onClick={() => setOpen((current) => !current)}
      >
        <ContextRing percent={percent} full={full} />
      </button>
      {open ? (
        <div
          id={titleId}
          role="dialog"
          aria-label="Context usage"
          className="absolute bottom-11 right-0 z-20 w-72 rounded-xl border border-line/80 bg-surface p-3 shadow-panel"
        >
          <div className="flex items-baseline justify-between gap-3">
            <p className="text-sm font-medium text-ink">
              {usage ? `${percent}% full` : 'Context'}
            </p>
            <p className="text-xs text-ink-faint">
              {usage
                ? `${usage.estimated ? '~' : ''}${formatTokens(used)} / ${formatTokens(limit)} tokens`
                : `${formatTokens(limit)} tokens`}
            </p>
          </div>
          {usage && rows.length > 0 ? (
            <>
              <div className="mt-2 flex h-1.5 overflow-hidden rounded-full bg-raised">
                {rows.map((row) => (
                  <span
                    key={row.id}
                    className={rowColor[row.id]}
                    style={{ width: `${(row.tokens / Math.max(used, limit, 1)) * 100}%` }}
                  />
                ))}
              </div>
              <ul className="mt-3 space-y-1.5">
                {rows.map((row) => (
                  <li key={row.id} className="flex items-center gap-2 text-xs text-ink-muted">
                    <span className={`h-2 w-2 shrink-0 rounded-sm ${rowColor[row.id]}`} />
                    <span className="min-w-0 flex-1">{row.label}</span>
                    <span className="tabular-nums text-ink-faint">{formatTokens(row.tokens)}</span>
                  </li>
                ))}
              </ul>
              {(usage.summarizedMessages ?? 0) > 0 ? (
                <p className="mt-2 text-xs leading-relaxed text-ink-muted">
                  The {usage.summarizedMessages} oldest messages were sent as a summary so the conversation fits. Every
                  message is still saved.
                </p>
              ) : null}
            </>
          ) : (
            <p className="mt-2 text-xs leading-relaxed text-ink-muted">
              Send a message to see how much of this model’s memory the chat is using.
            </p>
          )}
        </div>
      ) : null}
    </div>
  )
}

function ContextRing({ percent, full }: { percent: number; full: boolean }) {
  const size = 18
  const stroke = 2
  const radius = (size - stroke) / 2
  const circ = 2 * Math.PI * radius
  const shown = Math.max(0, Math.min(percent, 100))
  const dash = (shown / 100) * circ
  return (
    <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`} aria-hidden>
      <circle
        cx={size / 2}
        cy={size / 2}
        r={radius}
        fill="none"
        className="stroke-line"
        strokeWidth={stroke}
      />
      {shown > 0 ? (
        <circle
          cx={size / 2}
          cy={size / 2}
          r={radius}
          fill="none"
          className={full ? 'stroke-danger' : 'stroke-primary'}
          strokeWidth={stroke}
          strokeLinecap="round"
          strokeDasharray={`${dash} ${circ}`}
          transform={`rotate(-90 ${size / 2} ${size / 2})`}
        />
      ) : null}
    </svg>
  )
}
