import { useTranslation } from 'react-i18next'
import { useState } from 'react'
import { Link } from 'react-router-dom'
import { Ratatoskr } from '@/components/ui/Ratatoskr'
import { ReportProblem } from '@/components/ui/ReportProblem'
import { explainError } from './friendlyError'

/** A chat error in plain language, with the next step and the details on request. */
export function ChatErrorCard({
  raw,
  code,
  onRetry,
  onNewChat,
  mascot = true,
}: {
  raw: string
  /** The error's stable code from the service, when it sent one. */
  code?: string
  onRetry?: () => void
  onNewChat?: () => void
  /** Show Ratatoskr with the acorn dropped. Off when another mascot is already in view. */
  mascot?: boolean
}) {
  const { t } = useTranslation('chat')
  const [showDetail, setShowDetail] = useState(false)
  const e = explainError(raw, code)
  return (
    <div role="alert" className="flex max-w-[min(42rem,85%)] gap-3 rounded-2xl border border-danger/25 bg-danger/5 px-4 py-3 text-sm">
      {mascot ? <Ratatoskr state="error" size={48} className="-ms-1 mt-0.5" /> : null}
      <div className="min-w-0 flex-1">
        <p className="font-medium text-ink">{e.title}</p>
        <p className="mt-0.5 text-ink-muted">{e.body}</p>
        <div className="mt-2.5 flex flex-wrap items-center gap-2">
          {e.actions.includes('retry') && onRetry ? (
            <button type="button" className="btn-primary px-3 py-1 text-xs" onClick={onRetry}>
              {t('errors.tryAgain')}
            </button>
          ) : null}
          {e.actions.includes('new-chat') && onNewChat ? (
            <button type="button" className="btn-secondary px-3 py-1 text-xs" onClick={onNewChat}>
              {t('errors.startNew')}
            </button>
          ) : null}
          {e.actions.includes('models') ? (
            <Link to="/models" className="btn-secondary px-3 py-1 text-xs">
              {t('errors.openModels')}
            </Link>
          ) : null}
          {e.actions.includes('computers') ? (
            <Link to="/settings/computers" className="btn-secondary px-3 py-1 text-xs">
              {t('errors.openComputers')}
            </Link>
          ) : null}
          <ReportProblem error={raw} code={code} className="ms-auto text-xs text-ink-faint hover:text-ink" />
          {e.detail ? (
            <button type="button" className="text-xs text-ink-faint hover:text-ink" onClick={() => setShowDetail((v) => !v)}>
              {showDetail ? t('errors.hideDetails') : t('errors.details')}
            </button>
          ) : null}
        </div>
        {showDetail && e.detail ? <pre className="log-panel mt-2 whitespace-pre-wrap break-words text-xs">{e.detail}</pre> : null}
      </div>
    </div>
  )
}
