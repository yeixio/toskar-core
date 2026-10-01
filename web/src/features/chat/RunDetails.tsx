import { useState } from 'react'
import { api } from '@/lib/api'
import type { RunTrace } from '@/types/api'

function msText(ms?: number): string {
  if (!ms) return '—'
  return ms >= 1000 ? `${(ms / 1000).toFixed(1)} s` : `${Math.round(ms)} ms`
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
  const [open, setOpen] = useState(false)
  const [run, setRun] = useState<RunTrace | null>(null)
  const [error, setError] = useState('')

  const toggle = async () => {
    const next = !open
    setOpen(next)
    if (next && !run) {
      try {
        const r = await api.getRun(runId)
        if (r) setRun(r)
        else setError('This run is no longer recorded.')
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Could not load the run.')
      }
    }
  }

  return (
    <div className="text-xs text-ink-muted">
      <button type="button" className="underline-offset-2 hover:text-ink hover:underline" aria-expanded={open} onClick={() => void toggle()}>
        {open ? '▾' : '▸'} Run details
      </button>
      {open && error && <p className="mt-1 text-danger">{error}</p>}
      {open && run && (
        <dl className="mt-1.5 space-y-1 rounded-lg bg-raised/60 p-2.5">
          <Row label="Run">
            <span className="font-mono">{run.id.slice(0, 8)}</span> · {run.status}
            {run.error ? ` · ${run.error}` : ''}
          </Row>
          {run.strategy.length > 0 && <Row label="Strategy">{run.strategy.join(' · ')}</Row>}
          {run.effort && <Row label="Effort">{run.effort}</Row>}
          {run.models.map((m) => (
            <Row key={`${m.role}-${m.model_id}-${m.node}`} label={m.role ? `Model (${m.role})` : 'Model'}>
              {m.model_id}
              {m.node ? ` on ${m.node}` : ''} · {m.calls} {m.calls === 1 ? 'call' : 'calls'}
              {m.load_ms ? ` · load ${msText(m.load_ms)}` : ''} · first token {msText(m.first_token_ms)}
              {m.tok_per_sec ? ` · ${m.tok_per_sec.toFixed(1)} tok/s` : ''} · {m.prompt_tokens} in / {m.completion_tokens} out
              {m.cached_tokens ? ` · ${m.cached_tokens} cached` : ''}
            </Row>
          ))}
          {run.workers ? <Row label="Workers">{`${run.workers} ${run.parallel ? 'side by side' : 'in order'}`}</Row> : null}
          {run.tools.length > 0 && (
            <Row label="Tools">
              {run.tools
                .map((t) => `${t.tool_id} ×${t.calls} (${msText(t.total_ms)}${t.failures ? `, ${t.failures} failed` : ''})`)
                .join(', ')}
            </Row>
          )}
          {run.nodes.length > 0 && <Row label="Computers">{run.nodes.join(', ')}</Row>}
          {run.cache_hits && Object.keys(run.cache_hits).length > 0 && (
            <Row label="Cache hits">
              {Object.entries(run.cache_hits)
                .map(([tool, n]) => `${tool} ×${n}`)
                .join(', ')}
            </Row>
          )}
          <Row label="Verification">
            {run.verification_passes === 0
              ? 'none'
              : `${run.verification_passes} ${run.verification_passes === 1 ? 'pass' : 'passes'}${
                  run.verification_issues ? `, ${run.verification_issues} issues, ${run.verification_fixed ?? 0} fixed` : ''
                }`}
          </Row>
          <Row label="Retries">{run.retries}</Row>
          {run.context_tokens ? (
            <Row label="Context">
              {run.context_tokens}
              {run.context_limit ? ` of ${run.context_limit}` : ''} tokens
            </Row>
          ) : null}
          <Row label="Latency">
            {msText(run.latency_ms)}
            {run.pipeline_ms ? ` (${msText(run.pipeline_ms)} before the model)` : ''}
          </Row>
        </dl>
      )}
    </div>
  )
}
