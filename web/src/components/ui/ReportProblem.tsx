import { useMutation, useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useLocation } from 'react-router-dom'
import { api } from '@/lib/api'
import { isDesktopShell } from '@/lib/desktopBridge'
import { reportMailLink, reportText } from '@/lib/report'
import { useDialog } from '@/lib/useDialog'

/**
 * Report this: a link beside an error that shows everything a report
 * would send, lets the person add what they were doing, and sends it to
 * the Toskar team, by email when that can't be done here (#588).
 */
export function ReportProblem({ error, code, className }: { error: string; code?: string; className?: string }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  return (
    <>
      <button type="button" className={className ?? 'text-xs text-ink-faint hover:text-ink'} onClick={() => setOpen(true)}>
        {t('report.open')}
      </button>
      {open ? <ReportDialog error={error} code={code} onClose={() => setOpen(false)} /> : null}
    </>
  )
}

/** Why a report wasn't sent here: the computer's code, for what to offer instead. */
const errorCode = (err: unknown) => (err as { code?: string } | null)?.code ?? ''

function ReportDialog({ error, code, onClose }: { error: string; code?: string; onClose: () => void }) {
  const { t } = useTranslation()
  const ref = useDialog(true, onClose)
  const { pathname } = useLocation()
  const [note, setNote] = useState('')
  const [email, setEmail] = useState('')
  const [details, setDetails] = useState(true)
  const [copied, setCopied] = useState(false)
  const report = useQuery({
    queryKey: ['diagnostics', 'report'],
    queryFn: api.getReport,
    retry: false,
    staleTime: 60_000,
  })
  const settings = useQuery({ queryKey: ['settings'], queryFn: () => api.getSettings(), retry: false })
  const app = isDesktopShell() ? 'desktop' : 'web'
  const problem = { error, code, where: pathname, app }
  const included = details ? report.data : null
  const full = reportText(problem, note, included)
  const subject = `Toskar problem: ${code || pathname}`
  const mail = reportMailLink(subject, (n) => reportText(problem, note, included, n), included?.log?.length ?? 0)
  const send = useMutation({
    mutationFn: () =>
      api.sendReport({
        product: 'core',
        app: report.data?.version ? `${app} ${report.data.version}` : app,
        where: pathname,
        code,
        error,
        note: note.trim() || undefined,
        email: email.trim() || undefined,
        details,
      }),
  })
  // An Admin turned sending off: copy it, or give it to whoever runs Toskar.
  const off = settings.data?.problem_reports === false || errorCode(send.error) === 'REPORTS_OFF'
  const invalid = errorCode(send.error) === 'REPORT_INVALID'
  const sent = send.data
  const waiting = report.isPending && details
  return (
    <div className="fixed inset-0 z-50 flex items-end justify-center scrim p-4 sm:items-center" role="presentation" onClick={onClose}>
      <div
        ref={ref}
        className="card w-full max-w-lg space-y-3 shadow-panel"
        role="dialog"
        aria-modal="true"
        aria-labelledby="report-title"
        onClick={(event) => event.stopPropagation()}
      >
        <h2 id="report-title" className="font-display text-lg font-semibold text-ink">
          {t('report.title')}
        </h2>
        {sent ? (
          <>
            <p className="text-sm text-ink" role="status">
              {sent.status === 'added' ? t('report.sentAdded') : t('report.sent')}
            </p>
            <div className="flex justify-end">
              <button type="button" className="btn-primary" data-autofocus onClick={onClose}>
                {t('report.done')}
              </button>
            </div>
          </>
        ) : (
          <>
            <p className="text-sm text-ink-muted">{off ? t('report.off') : t('report.body')}</p>
            <label className="block text-sm font-medium text-ink">
              {t('report.noteLabel')}
              <textarea
                className="field mt-1 min-h-[4.5rem] w-full font-normal"
                value={note}
                placeholder={t('report.notePlaceholder')}
                data-autofocus
                onChange={(event) => setNote(event.target.value)}
              />
            </label>
            {off ? null : (
              <label className="block text-sm font-medium text-ink">
                {t('report.emailLabel')}
                <input
                  type="email"
                  className="field mt-1 w-full font-normal"
                  value={email}
                  autoComplete="email"
                  onChange={(event) => setEmail(event.target.value)}
                />
                <span className="mt-1 block text-xs font-normal text-ink-faint">{t('report.emailHint')}</span>
              </label>
            )}
            <label className="flex items-start gap-2 text-sm text-ink">
              <input type="checkbox" className="mt-0.5" checked={details} onChange={(event) => setDetails(event.target.checked)} />
              <span>
                <span className="font-medium">{t('report.details')}</span>
                <span className="block text-xs text-ink-muted">{t('report.detailsHint')}</span>
              </span>
            </label>
            <div>
              <p className="text-xs font-medium text-ink-muted">{t('report.whatSends')}</p>
              <pre className="log-panel mt-1 max-h-56 overflow-auto whitespace-pre-wrap break-words text-xs">
                {waiting ? t('report.gathering') : full}
              </pre>
            </div>
            {send.isError && !off ? (
              <p className="text-sm text-warning" role="alert">
                {invalid ? t('report.invalid') : t('report.failed')}
              </p>
            ) : null}
            <div className="flex flex-wrap items-center justify-end gap-2">
              <button type="button" className="btn-secondary me-auto" onClick={onClose}>
                {t('report.cancel')}
              </button>
              <button
                type="button"
                className="btn-secondary"
                disabled={waiting}
                onClick={() => {
                  void navigator.clipboard.writeText(full).then(() => setCopied(true))
                }}
              >
                {copied ? t('report.copied') : t('report.copy')}
              </button>
              {off ? null : send.isError && !invalid ? (
                // The service couldn't take it: the same report, by email.
                <a href={mail} className="btn-primary">
                  {t('report.sendEmail')}
                </a>
              ) : (
                <button type="button" className="btn-primary" disabled={waiting || send.isPending} onClick={() => send.mutate()}>
                  {send.isPending ? t('report.sending') : t('report.send')}
                </button>
              )}
            </div>
            {off || send.isError ? null : (
              <p className="text-end text-xs text-ink-faint">
                <a href={mail} className="underline hover:text-ink">
                  {t('report.orEmail')}
                </a>
              </p>
            )}
          </>
        )}
      </div>
    </div>
  )
}
