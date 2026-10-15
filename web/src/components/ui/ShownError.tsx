import { errorText } from '@/lib/api'
import { ReportProblem } from './ReportProblem'

/**
 * An error a record or an action shows: its text in the App language from
 * its code, when it has one, and Report this with the code (#588).
 */
export function ShownError({
  error,
  code,
  details,
  className = 'text-sm text-danger',
}: {
  error: string
  code?: string
  details?: Record<string, unknown>
  className?: string
}) {
  return (
    <p className={className}>
      {errorText(code, error, details)}
      <ReportProblem error={error} code={code} className="ms-2 text-xs text-ink-faint underline hover:text-ink" />
    </p>
  )
}

/** A failed request's error for ShownError: the service's text, code, and values. */
export function shownError(error: unknown): { error: string; code?: string; details?: Record<string, unknown> } {
  const e = error as { serviceMessage?: unknown; message?: unknown; code?: unknown; details?: unknown } | null
  const text = typeof e?.serviceMessage === 'string' ? e.serviceMessage : typeof e?.message === 'string' ? e.message : String(error)
  return {
    error: text,
    code: typeof e?.code === 'string' ? e.code : undefined,
    details: e?.details && typeof e.details === 'object' ? (e.details as Record<string, unknown>) : undefined,
  }
}
