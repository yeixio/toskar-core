import { useTranslation } from 'react-i18next'
import { useState } from 'react'
import { formatMilliseconds, formatTokensPerSecond } from '@/i18n/format'
import { api, errorText } from '@/lib/api'
import type { RunTrace } from '@/types/api'
import { roleLabel, roleOrder } from './runRoles'
import { ShownError, shownOr, type Shown } from '@/components/ui/ShownError'

function msText(ms?: number): string {
  if (!ms) return '—'
  return formatMilliseconds(ms)
}

/** One labelled row of the run details. */
function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="grid grid-cols-[7.5rem_1fr] gap-2">
      <dt className="text-ink-faint">{label}</dt>
      <dd className="min-w-0 text-ink-muted">{children}</dd>
    </div>
  )
}

/**
 * Advanced run details (spec §35): the strategy, models, tools, and
 * computers behind an answer, and how long each part took. Loaded when
 * opened.
 */
export function RunDetails({ runId }: { runId: string }) {
  const { t } = useTranslation('chat')
  const [open, setOpen] = useState(false)
  const [run, setRun] = useState<RunTrace | null>(null)
  const [error, setError] = useState<Shown | null>(null)

  const toggle = async () => {
    const next = !open
    setOpen(next)
    if (next && !run) {
      try {
        const r = await api.getRun(runId)
        if (r) setRun(r)
        else setError({ error: t('run.gone') })
      } catch (err) {
        setError(shownOr(err, t('run.loadFailed')))
      }
    }
  }

  return (
    <div className="text-xs text-ink-muted">
      <button type="button" className="underline-offset-2 hover:text-ink hover:underline" aria-expanded={open} onClick={() => void toggle()}>
        {open ? '▾' : '▸'} {t('run.details')}
      </button>
      {open && error && <ShownError {...error} className="mt-1 text-danger" />}
      {open && run && (
        <dl className="mt-1.5 space-y-1 rounded-lg bg-raised/60 p-2.5">
          <Row label={t('run.run')}>
            <span className="font-mono">{run.id.slice(0, 8)}</span> · {t(`run.status.${run.status}`, { defaultValue: run.status })}
            {run.error ? (
              <>
                {' · '}
                <span title={run.error_code ? run.error : undefined}>{errorText(run.error_code, run.error, run.error_details)}</span>
              </>
            ) : null}
          </Row>
          {run.strategy.length > 0 && <Row label={t('run.strategy')}>{run.strategy.join(' · ')}</Row>}
          {run.effort && <Row label={t('run.effort')}>{run.effort}</Row>}
          {[...run.models]
            .sort((a, b) => roleOrder(a.role) - roleOrder(b.role))
            .map((m) => (
              <Row key={`${m.role}-${m.model_id}-${m.node}`} label={roleLabel(m.role)}>
                {m.model_id}
                {m.node ? ` ${t('run.onComputer', { computer: m.node })}` : ''} · {t('run.calls', { count: m.calls })}
                {m.load_ms ? ` · ${t('run.load', { time: msText(m.load_ms) })}` : ''} · {t('run.firstToken', { time: msText(m.first_token_ms) })}
                {m.tok_per_sec ? ` · ${formatTokensPerSecond(m.tok_per_sec)}` : ''} ·{' '}
                {t('run.inOut', { in: m.prompt_tokens, out: m.completion_tokens })}
                {m.cached_tokens ? ` · ${t('run.cached', { count: m.cached_tokens })}` : ''}
              </Row>
            ))}
          {run.models.length > 1 && (
            <Row label={t('run.modelCalls')}>{run.models.reduce((n, m) => n + m.calls, 0)}</Row>
          )}
          {run.workers ? (
            <Row label={t('run.workers')}>
              {t(run.parallel ? 'run.workersParallel' : 'run.workersInOrder', { count: run.workers })}
            </Row>
          ) : null}
          {run.tools.length > 0 && (
            <Row label={t('run.tools')}>
              {run.tools
                .map(
                  (tool) =>
                    `${tool.tool_id} ×${tool.calls} (${msText(tool.total_ms)}${tool.failures ? `, ${t('run.toolFailures', { count: tool.failures })}` : ''})`,
                )
                .join(', ')}
            </Row>
          )}
          {run.nodes.length > 0 && <Row label={t('run.computers')}>{run.nodes.join(', ')}</Row>}
          {run.cache_hits && Object.keys(run.cache_hits).length > 0 && (
            <Row label={t('run.cacheHits')}>
              {Object.entries(run.cache_hits)
                .map(([tool, n]) => `${tool} ×${n}`)
                .join(', ')}
            </Row>
          )}
          <Row label={t('run.verification')}>
            {run.verification_passes === 0
              ? t('run.verificationNone')
              : `${t('run.verificationPasses', { count: run.verification_passes })}${
                  run.verification_issues
                    ? `, ${t('run.verificationIssues', { issues: run.verification_issues, fixed: run.verification_fixed ?? 0 })}`
                    : ''
                }`}
          </Row>
          <Row label={t('run.retries')}>{run.retries}</Row>
          {run.context_tokens ? (
            <Row label={t('run.context')}>
              {run.context_limit
                ? t('run.contextTokensOf', { tokens: run.context_tokens, limit: run.context_limit })
                : t('run.contextTokens', { tokens: run.context_tokens })}
            </Row>
          ) : null}
          <Row label={t('run.latency')}>
            {msText(run.latency_ms)}
            {run.pipeline_ms ? ` ${t('run.beforeModel', { time: msText(run.pipeline_ms) })}` : ''}
          </Row>
        </dl>
      )}
    </div>
  )
}
