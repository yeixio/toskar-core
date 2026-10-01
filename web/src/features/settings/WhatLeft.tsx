import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import type { EgressKind, EgressRecord } from '@/types/api'

const KIND_LABEL: Record<EgressKind, string> = {
  web_search: 'Web search',
  web_page: 'Web page',
  paired_computer: 'Paired computer',
  external_server: 'External server',
  connector: 'Connected service',
}

const SOURCE_LABEL: Record<string, string> = {
  chat: 'Chat',
  api: 'API',
  automation: 'Automation',
  training: 'Training',
}

const RETENTION: [number, string][] = [
  [7, '7 days'],
  [30, '30 days'],
  [90, '90 days'],
  [365, '1 year'],
  [0, 'Keep them'],
]

function when(iso: string): string {
  return new Date(iso).toLocaleString(undefined, { month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' })
}

/**
 * What left this computer (spec §63): every web search, page read, paired
 * computer, external server, and connected service runs sent data to, and
 * how long run records are kept.
 */
export function WhatLeft() {
  const queryClient = useQueryClient()
  const overview = useQuery({ queryKey: ['privacy'], queryFn: () => api.getPrivacy(), retry: false })
  const records = useQuery({ queryKey: ['egress'], queryFn: () => api.listEgress(), retry: false })
  const [message, setMessage] = useState('')

  const retention = useMutation({
    mutationFn: (days: number) => api.setRunRetention(days),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['privacy'] })
      void queryClient.invalidateQueries({ queryKey: ['egress'] })
    },
  })
  const deleteRuns = useMutation({
    mutationFn: () => api.deleteRunRecords(),
    onSuccess: (c) => {
      setMessage(
        c ? `Deleted ${c.tasks} run records, ${c.automation_runs} automation results, and ${c.egress} entries below.` : '',
      )
      void queryClient.invalidateQueries({ queryKey: ['privacy'] })
      void queryClient.invalidateQueries({ queryKey: ['egress'] })
    },
  })

  const counts = overview.data?.last_30_days ?? {}
  const total = Object.values(counts).reduce((a, b) => a + (b ?? 0), 0)
  const list: EgressRecord[] = records.data ?? []

  return (
    <section className="card space-y-4">
      <div>
        <h2 className="section-title">What left this computer</h2>
        <p className="mt-1 text-sm text-ink-muted">
          Yggdrasil works on this computer. These are the times a chat, automation, or app sent something elsewhere:
          web searches and pages, paired computers, and connected services. Mark memories or knowledge
          &ldquo;This computer only&rdquo; to keep chats that use them here.
        </p>
      </div>

      <p className="text-sm text-ink">
        {total === 0
          ? 'Nothing left this computer in the last 30 days.'
          : `In the last 30 days: ${(Object.keys(KIND_LABEL) as EgressKind[])
              .filter((k) => counts[k])
              .map((k) => `${KIND_LABEL[k].toLowerCase()} ${counts[k]}`)
              .join(', ')}.`}
      </p>

      {list.length > 0 && (
        <ul className="max-h-72 divide-y divide-line/50 overflow-y-auto rounded-lg border border-line/60">
          {list.map((r) => (
            <li key={r.id} className="flex flex-wrap items-baseline gap-x-2 px-3 py-2 text-xs">
              <span className="font-medium text-ink">{KIND_LABEL[r.kind] ?? r.kind}</span>
              <span className="text-ink">{r.destination}</span>
              {r.detail && <span className="min-w-0 flex-1 truncate text-ink-muted" title={r.detail}>{r.detail}</span>}
              <span className="ml-auto shrink-0 text-ink-faint">
                {r.source ? `${SOURCE_LABEL[r.source] ?? r.source} · ` : ''}
                {when(r.at)}
              </span>
            </li>
          ))}
        </ul>
      )}

      <div className="flex flex-wrap items-end gap-3">
        <label className="block text-sm">
          <span className="text-ink-muted">Keep run records for</span>
          <select
            className="field mt-1"
            value={overview.data?.retention_days ?? 30}
            disabled={retention.isPending}
            onChange={(e) => retention.mutate(Number(e.target.value))}
          >
            {RETENTION.map(([days, label]) => (
              <option key={days} value={days}>
                {label}
              </option>
            ))}
          </select>
        </label>
        <button
          type="button"
          className="btn-secondary px-3 py-1.5 text-xs"
          disabled={deleteRuns.isPending}
          onClick={() => {
            if (
              window.confirm(
                'Delete run records now? This removes stored prompts and tool results from past runs, and this list. Chats are not deleted.',
              )
            ) {
              deleteRuns.mutate()
            }
          }}
        >
          Delete run records now
        </button>
      </div>
      <p className="text-xs text-ink-faint">
        Run records hold prompts and tool results from past runs. Chats are kept or not by History below. Each automation
        keeps its latest result, which &ldquo;notify when the result changes&rdquo; compares against.
      </p>
      {message && <p className="text-xs text-ink-muted">{message}</p>}
      {(retention.error || deleteRuns.error) && (
        <p className="text-xs text-danger">{String((retention.error ?? deleteRuns.error) as Error)}</p>
      )}
    </section>
  )
}
