import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { ReportProblem } from './ReportProblem'

interface LoadErrorProps {
  /** The failed query's error; its message shows under Details. */
  error: unknown
  onRetry: () => void
  retrying?: boolean
  /** One line, for a panel or card inside a page rather than the page itself. */
  compact?: boolean
}

/**
 * What a page shows when its data could not be loaded, in place of its
 * empty state, so a failed request never looks like "you have none".
 */
export function LoadError({ error, onRetry, retrying, compact }: LoadErrorProps) {
  const { t } = useTranslation()
  const detail = error instanceof Error ? error.message : typeof error === 'string' ? error : ''
  if (compact) {
    return (
      <p role="alert" className="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm text-danger">
        <span title={detail || undefined}>{t('loadError.title')}</span>
        <button type="button" className="font-medium text-primary hover:underline disabled:opacity-60" disabled={retrying} onClick={onRetry}>
          {retrying ? t('loadError.retrying') : t('loadError.retry')}
        </button>
        <ReportProblem error={detail} code={errorCode(error)} />
      </p>
    )
  }
  return (
    <div role="alert" className="empty-hero max-w-lg">
      <h2 className="font-display text-xl font-semibold text-ink">{t('loadError.title')}</h2>
      <p className="text-[15px] leading-relaxed text-ink-muted">{t('loadError.body')}</p>
      <div className="flex flex-wrap items-center gap-3 pt-2">
        <button type="button" className="btn-primary" disabled={retrying} onClick={onRetry}>
          {retrying ? t('loadError.retrying') : t('loadError.retry')}
        </button>
        <Link to="/diagnostics" className="text-sm font-medium text-primary hover:underline">
          {t('loadError.diagnostics')}
        </Link>
        <ReportProblem error={detail} code={errorCode(error)} className="text-sm font-medium text-primary hover:underline" />
      </div>
      {detail ? (
        <details className="pt-2 text-xs text-ink-faint">
          <summary className="cursor-pointer">{t('loadError.details')}</summary>
          <p className="mt-1 break-words font-mono">{detail}</p>
        </details>
      ) : null}
    </div>
  )
}

/** The service's stable code on an ApiError, read without importing it so a page's mocked api still works. */
function errorCode(error: unknown): string | undefined {
  const code = (error as { code?: unknown } | null)?.code
  return typeof code === 'string' ? code : undefined
}
