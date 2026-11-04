import { useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { plainOutput } from './outputText'

/** What a task's command is writing, following the end unless scrolled back. */
export function CommandOutput({ text }: { text: string }) {
  const { t } = useTranslation('chat')
  const ref = useRef<HTMLPreElement>(null)
  const pinned = useRef(true)
  const shown = plainOutput(text).replace(/\n+$/, '')
  useEffect(() => {
    if (ref.current && pinned.current) ref.current.scrollTop = ref.current.scrollHeight
  }, [shown])
  if (!shown) return null
  return (
    <pre
      ref={ref}
      tabIndex={0}
      aria-label={t('task.output')}
      data-testid="command-output"
      className="mt-2 max-h-40 overflow-auto whitespace-pre-wrap break-words rounded-xl bg-raised/80 px-4 py-3 font-mono text-xs text-ink-muted"
      onScroll={(event) => {
        const el = event.currentTarget
        pinned.current = el.scrollHeight - el.scrollTop - el.clientHeight < 8
      }}
    >
      {shown}
    </pre>
  )
}
