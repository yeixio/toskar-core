import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import { api } from '@/lib/api'
import type { ToolRecord, ToolRun } from '@/types/api'
import { RealmKicker } from '@/components/ui/Realm'
import { ToolSources } from './ToolSources'

const FILTERS = ['All', 'Built-in', 'Added', 'Disabled'] as const

/** Test arguments for a tool: its required fields, left for you to fill. */
function exampleArgs(tool: ToolRecord): string {
  if (tool.id === 'internet.search') return '{"query":"Juneau AK weather"}'
  try {
    const schema = JSON.parse(tool.schema) as Record<string, string>
    const out: Record<string, string> = {}
    for (const key of Object.keys(schema)) if (!key.endsWith('?')) out[key] = ''
    return JSON.stringify(out)
  } catch {
    return '{}'
  }
}

/** Where a tool comes from, in words. */
function sourceLabel(source: string): string {
  if (source === 'builtin') return 'built in'
  const [kind, id] = source.split(':')
  return kind === 'mcp' ? `tool source ${id}` : kind === 'connector' ? `connected service ${id}` : source
}

export function ToolsPage() {
  const queryClient = useQueryClient()
  const [query, setQuery] = useState('')
  const [filter, setFilter] = useState<(typeof FILTERS)[number]>('All')
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [testArgs, setTestArgs] = useState('{"query":"Juneau AK weather"}')
  const [testOutput, setTestOutput] = useState('')

  const toolsQuery = useQuery({
    queryKey: ['tools'],
    queryFn: () => api.listTools(),
  })
  const tools = toolsQuery.data ?? []
  const selected = tools.find((tool) => tool.id === selectedId) ?? null

  const visible = useMemo(() => {
    const needle = query.trim().toLowerCase()
    return tools.filter((tool) => {
      if (filter === 'Built-in' && tool.source !== 'builtin') return false
      if (filter === 'Added' && tool.source === 'builtin') return false
      if (filter === 'Disabled' && tool.enabled) return false
      if (!needle) return true
      return [tool.name, tool.description, tool.capability, tool.source, tool.id]
        .join(' ')
        .toLowerCase()
        .includes(needle)
    })
  }, [tools, query, filter])

  const toggle = useMutation({
    mutationFn: (tool: ToolRecord) => api.setToolEnabled(tool.id, !tool.enabled),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['tools'] }),
  })

  const test = useMutation({
    mutationFn: () => {
      const args = JSON.parse(testArgs) as Record<string, unknown>
      return api.testTool(selected?.id || '', args)
    },
    onSuccess: (result) => setTestOutput(JSON.stringify(result, null, 2)),
    onError: (error) => setTestOutput(error instanceof Error ? error.message : 'Test failed'),
  })

  return (
    <div className="page-fill gap-4 overflow-y-auto p-4">
      <div>
        <RealmKicker />
        <h1 className="font-display text-2xl font-semibold text-ink">Tools</h1>
        <p className="mt-1 max-w-2xl text-sm text-ink-muted">
          What the AI can do besides answering: search the web, work with files, and use the apps and services you add.
        </p>
      </div>
      <ToolSources />
      <div>
        <h2 className="section-title">All tools</h2>
        <p className="mt-1 max-w-2xl text-sm text-ink-muted">
          Every tool, built in or added. Turn one off here to keep it out of every chat.
        </p>
      </div>
      <div className="flex flex-wrap items-center gap-2">
        <input
          className="field min-w-[16rem] flex-1"
          value={query}
          placeholder="Search tools…"
          onChange={(event) => setQuery(event.target.value)}
        />
        {FILTERS.map((item) => (
          <button
            key={item}
            type="button"
            className={filter === item ? 'btn-primary px-3 py-1.5 text-xs' : 'btn-secondary px-3 py-1.5 text-xs'}
            onClick={() => setFilter(item)}
          >
            {item}
          </button>
        ))}
      </div>
      <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(18rem,24rem)]">
        <ul className="space-y-2">
          {toolsQuery.isLoading && <li className="text-sm text-ink-muted">Loading tools…</li>}
          {visible.map((tool) => (
            <li key={tool.id}>
              <button
                type="button"
                className="card w-full text-left"
                onClick={() => {
                  setSelectedId(tool.id)
                  setTestOutput('')
                  setTestArgs(exampleArgs(tool))
                }}
              >
                <div className="flex items-start justify-between gap-3">
                  <div>
                    <p className="font-medium text-ink">{tool.name}</p>
                    <p className="mt-1 text-xs text-ink-muted">{tool.description}</p>
                  </div>
                  <span className={tool.enabled ? 'text-xs text-success' : 'text-xs text-ink-faint'}>
                    {tool.enabled ? 'Enabled' : 'Disabled'}
                  </span>
                </div>
                <p className="mt-2 text-xs text-ink-faint">
                  {tool.capability} · {sourceLabel(tool.source)}
                </p>
              </button>
            </li>
          ))}
        </ul>
        {selected && (
          <aside className="card h-fit space-y-3">
            <h2 className="font-display text-lg font-semibold text-ink">{selected.name}</h2>
            <p className="text-sm text-ink-muted">{selected.description}</p>
            <dl className="space-y-1 text-xs text-ink-muted">
              <Row label="Source" value={sourceLabel(selected.source)} />
              <Row label="Capability" value={selected.capability} />
              <Row label="Permission default" value={selected.default_policy} />
              {selected.level && <Row label="Level" value={`${selected.level} · ${selected.level_name ?? ''}`} />}
              {selected.outputs && <Row label="Returns" value={selected.outputs.join(', ')} />}
              {selected.timeout_seconds ? <Row label="Time limit" value={`${selected.timeout_seconds} s`} /> : null}
              {selected.requirements && <Row label="Needs" value={needs(selected) || 'Nothing else'} />}
              {selected.health && <Row label="Health" value={HEALTH[selected.health]} />}
              <Row label="Profiles" value={selected.profiles.join(', ') || 'None'} />
            </dl>
            <pre className="log-panel text-xs">{selected.schema}</pre>
            <button
              type="button"
              className="btn-secondary px-3 py-1.5 text-xs"
              disabled={toggle.isPending}
              onClick={() => toggle.mutate(selected)}
            >
              {selected.enabled ? 'Disable' : 'Enable'}
            </button>
            <RecentCalls toolId={selected.id} />
            {selected.risk === 'read' && (
              <div className="space-y-2">
                <textarea
                  className="field min-h-20 w-full font-mono text-xs"
                  value={testArgs}
                  onChange={(event) => setTestArgs(event.target.value)}
                />
                <button
                  type="button"
                  className="btn-primary px-3 py-1.5 text-xs"
                  disabled={test.isPending}
                  onClick={() => test.mutate()}
                >
                  Test tool
                </button>
                {testOutput && <pre className="log-panel max-h-64 text-xs">{testOutput}</pre>}
              </div>
            )}
          </aside>
        )}
      </div>
    </div>
  )
}

const HEALTH: Record<NonNullable<ToolRecord['health']>, string> = {
  ok: 'Ready',
  off: 'Turned off',
  unavailable: 'Not running',
}

/** What a tool needs besides itself, in words. */
function needs(tool: ToolRecord): string {
  const r = tool.requirements
  if (!r) return ''
  return [r.network && 'internet', r.filesystem && 'files on this computer', r.credentials && 'a stored credential', r.runtime, r.gpu && 'a GPU']
    .filter(Boolean)
    .join(', ')
}

const STATUS: Record<ToolRun['status'], string> = {
  completed: 'ran',
  cached: 'answered from cache',
  failed: 'failed',
  denied: 'not allowed',
  refused: 'refused',
  disabled: 'turned off',
}

/** The tool's latest audited calls (Gungnir §13). */
function RecentCalls({ toolId }: { toolId: string }) {
  const runs = useQuery({ queryKey: ['tool-runs', toolId], queryFn: () => api.listToolRuns(toolId), retry: false })
  const list = runs.data ?? []
  return (
    <div className="space-y-1">
      <p className="text-xs font-medium text-ink-muted">Recent calls</p>
      {list.length === 0 ? (
        <p className="text-xs text-ink-faint">None recorded.</p>
      ) : (
        <ul className="space-y-1 text-xs">
          {list.map((run) => (
            <li key={run.id} className="flex justify-between gap-2">
              <span className="min-w-0 truncate text-ink-muted" title={run.error || run.summary}>
                {new Date(run.at).toLocaleString()} · {STATUS[run.status]}
                {run.approval === 'you' ? ' (you approved)' : run.approval === 'session' ? ' (allowed for the session)' : ''}
                {run.summary ? ` · ${run.summary}` : ''}
              </span>
              {run.duration_ms > 0 && <span className="shrink-0 text-ink-faint">{run.duration_ms} ms</span>}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex justify-between gap-3">
      <dt>{label}</dt>
      <dd className="text-right text-ink">{value}</dd>
    </div>
  )
}
