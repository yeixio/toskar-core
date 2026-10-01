import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { EmptyState } from '@/components/ui/EmptyState'
import { api, ApiError } from '@/lib/api'
import { subscribeEvents } from '@/lib/events'
import { readScreenshotLaunch } from '@/lib/screenshotMode'
import type { Automation, AutomationDetail, AutomationInput, AutomationRun, Model } from '@/types/api'
import { AutomationForm } from './AutomationForm'
import { clockDetail, compactWhen, explainRun, runTiming } from './display'
import { notificationLabel, resultProse, scheduleLabel, visibleTask } from './parseRequest'
import { RealmKicker } from '@/components/ui/Realm'

const screenshotSentence =
  'Every morning at 8:00 AM, check this product and tell me if the price is below $500.'

export function AutomationsPage() {
  const queryClient = useQueryClient()
  const [selectedID, setSelectedID] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)
  const [editing, setEditing] = useState(false)
  const [formError, setFormError] = useState('')
  const [query, setQuery] = useState('')
  const [filter, setFilter] = useState<'all' | 'active' | 'paused' | 'attention'>('all')

  const listQuery = useQuery({
    queryKey: ['automations'],
    queryFn: () => api.listAutomations(),
  })
  const profilesQuery = useQuery({
    queryKey: ['profiles'],
    queryFn: () => api.getProfiles(),
  })
  const toolsQuery = useQuery({
    queryKey: ['tools'],
    queryFn: () => api.listTools(),
  })
  const modelsQuery = useQuery({
    queryKey: ['models'],
    queryFn: () => api.getModels(),
  })
  const detailQuery = useQuery({
    queryKey: ['automation', selectedID],
    queryFn: () => api.getAutomation(selectedID ?? ''),
    enabled: Boolean(selectedID),
  })

  useEffect(() => {
    return subscribeEvents({
      onEvent: (event) => {
        if (!event.type.startsWith('automation.')) return
        void queryClient.invalidateQueries({ queryKey: ['automations'] })
        void queryClient.invalidateQueries({ queryKey: ['automation'] })
      },
    })
  }, [queryClient])

  const items = (listQuery.data ?? []).filter((item) => matchesAutomation(item, query, filter))
  const detail = detailQuery.data
  const profiles = profilesQuery.data ?? []
  const tools = toolsQuery.data ?? []
  const models = modelsQuery.data ?? []

  useEffect(() => {
    if (!readScreenshotLaunch()?.enabled) return
    const compose = new URLSearchParams(window.location.search).get('compose') === '1'
    if (compose) {
      setCreating(true)
      return
    }
    if (!selectedID && items[0]) setSelectedID(items[0].id)
  }, [items, selectedID])

  function refresh() {
    void queryClient.invalidateQueries({ queryKey: ['automations'] })
    void queryClient.invalidateQueries({ queryKey: ['automation'] })
  }

  const save = useMutation({
    mutationFn: async (input: AutomationInput) => {
      if (editing && selectedID) return api.updateAutomation(selectedID, input)
      return api.createAutomation(input)
    },
    onSuccess: (saved) => {
      setFormError('')
      setCreating(false)
      setEditing(false)
      if (saved?.id) setSelectedID(saved.id)
      refresh()
    },
    onError: (error) => setFormError(error instanceof ApiError ? error.message : 'Could not save the automation.'),
  })

  const pause = useMutation({
    mutationFn: (item: Automation) => (item.enabled ? api.pauseAutomation(item.id) : api.resumeAutomation(item.id)),
    onSuccess: refresh,
  })
  const remove = useMutation({
    mutationFn: (id: string) => api.deleteAutomation(id),
    onSuccess: () => {
      setSelectedID(null)
      setEditing(false)
      refresh()
    },
  })
  const runNow = useMutation({
    mutationFn: (id: string) => api.runAutomation(id),
    onSuccess: refresh,
  })

  return (
    <div className="page-fill gap-4 overflow-y-auto p-4">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <RealmKicker />
          <h1 className="font-display text-2xl font-semibold text-ink">Automations</h1>
          <p className="mt-1 max-w-2xl text-sm text-ink-muted">
            Automations run in the background, even when this window is closed. Schedule a recurring task, or have Yggdrasil tell you when something changes.
          </p>
        </div>
        <button
          type="button"
          className="btn-primary px-3 py-1.5 text-xs"
          onClick={() => {
            setCreating(true)
            setEditing(false)
            setSelectedID(null)
            setFormError('')
          }}
        >
          New automation
        </button>
      </div>
      <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(20rem,32rem)]">
        <section className="space-y-3">
          <div className="flex flex-wrap items-center gap-2">
            <input
              className="field min-w-40 flex-1"
              value={query}
              placeholder="Search"
              aria-label="Search automations"
              onChange={(event) => setQuery(event.target.value)}
            />
            {(['all', 'active', 'paused', 'attention'] as const).map((key) => (
              <button
                key={key}
                type="button"
                className={filter === key ? 'btn-primary px-3 py-1.5 text-xs' : 'btn-secondary px-3 py-1.5 text-xs'}
                onClick={() => setFilter(key)}
              >
                {key === 'all' ? 'All' : key === 'active' ? 'Active' : key === 'paused' ? 'Paused' : 'Needs attention'}
              </button>
            ))}
          </div>
          {listQuery.isLoading && <p className="text-sm text-ink-muted">Loading automations…</p>}
          {listQuery.isError && <p className="text-sm text-danger">Automations could not be loaded.</p>}
          {!listQuery.isLoading && (listQuery.data ?? []).length === 0 && !creating && (
            <EmptyState
              title="No automations yet"
              description="Describe a recurring check, such as a morning price or a Friday release summary, and Yggdrasil will run it on that schedule."
            />
          )}
          {!listQuery.isLoading && (listQuery.data ?? []).length > 0 && items.length === 0 && (
            <p className="text-sm text-ink-muted">No automations match.</p>
          )}
          <ul className="space-y-2">
            {items.map((item) => (
              <li key={item.id}>
                <button
                  type="button"
                  className={[
                    'selectable w-full',
                    selectedID === item.id ? 'shadow-[inset_0_0_0_1.5px_rgb(var(--rgb-primary))]' : '',
                  ].filter(Boolean).join(' ')}
                  onClick={() => {
                    setSelectedID(item.id)
                    setCreating(false)
                    setEditing(false)
                    setFormError('')
                  }}
                >
                  <div className="flex items-start justify-between gap-3">
                    <p className="font-medium text-ink">{item.name}</p>
                    <StatusPill item={item} />
                  </div>
                  <p className="mt-1 text-xs text-ink-muted">{scheduleLabel(item.schedule)}</p>
                  <p className="mt-2 text-xs text-ink-faint">
                    Last {compactWhen(item.last_run_at, item.schedule.time_zone)}
                    {' · Next '}
                    {compactWhen(item.next_run_at, item.schedule.time_zone)}
                  </p>
                  {resultProse(item.last_result) && <p className="mt-2 line-clamp-2 text-sm text-ink-muted">{resultProse(item.last_result)}</p>}
                </button>
              </li>
            ))}
          </ul>
        </section>
        <aside>
          {creating || editing ? (
            <AutomationForm
              key={editing ? selectedID ?? 'edit' : 'new'}
              profiles={profiles}
              models={models}
              tools={tools}
              initial={editing ? detail : null}
              seedDescription={
                creating && new URLSearchParams(window.location.search).get('compose') === '1'
                  ? screenshotSentence
                  : ''
              }
              pending={save.isPending}
              error={formError}
              onCancel={() => {
                setCreating(false)
                setEditing(false)
                setFormError('')
              }}
              onSubmit={(input) => save.mutate(input)}
            />
          ) : detail ? (
            <Detail
              detail={detail}
              models={models}
              running={runNow.isPending}
              runError={runNow.error instanceof Error ? runNow.error.message : ''}
              onRun={() => runNow.mutate(detail.id)}
              onToggle={() => pause.mutate(detail)}
              onEdit={() => {
                setEditing(true)
                setFormError('')
              }}
              onDelete={() => {
                if (window.confirm(`Delete “${detail.name}”? Its history is removed too.`)) {
                  remove.mutate(detail.id)
                }
              }}
            />
          ) : (
            <div className="card text-sm text-ink-muted">Select an automation to see its history, or create one.</div>
          )}
        </aside>
      </div>
    </div>
  )
}

function Detail({
  detail,
  models,
  running,
  runError,
  onRun,
  onToggle,
  onEdit,
  onDelete,
}: {
  detail: AutomationDetail
  models: Model[]
  running: boolean
  runError: string
  onRun: () => void
  onToggle: () => void
  onEdit: () => void
  onDelete: () => void
}) {
  const zone = detail.schedule.time_zone
  return (
    <div className="card space-y-4">
      <div>
        <div className="flex items-start justify-between gap-3">
          <h2 className="font-display text-lg font-semibold text-ink">{detail.name}</h2>
          <StatusPill item={detail} />
        </div>
        <div className="mt-4 space-y-3 text-sm">
          <div>
            <p className="text-xs tracking-wide text-ink-faint">Schedule</p>
            <p className="text-ink">{scheduleLabel(detail.schedule)}</p>
            <p className="text-ink-muted">Next {compactWhen(detail.next_run_at, zone)}</p>
          </div>
          <div>
            <p className="text-xs tracking-wide text-ink-faint">Task</p>
            <p className="whitespace-pre-wrap text-ink">{visibleTask(detail.prompt)}</p>
          </div>
          <div>
            <p className="text-xs tracking-wide text-ink-faint">Notification</p>
            <p className="text-ink">{notificationLabel(detail.notification)}</p>
            <p className="text-ink-muted">{detail.notification.mode === 'none' ? 'Stored on this computer' : 'On this computer'}</p>
          </div>
        </div>
        {detail.last_error && <p className="mt-3 text-sm text-danger">{detail.last_error}</p>}
      </div>
      <div className="flex flex-wrap gap-2">
        <button type="button" className="btn-primary px-3 py-1.5 text-xs" disabled={running} onClick={onRun}>
          {running ? 'Running…' : 'Run now'}
        </button>
        <button type="button" className="btn-secondary px-3 py-1.5 text-xs" onClick={onToggle}>
          {detail.enabled ? 'Pause' : 'Resume'}
        </button>
        <button type="button" className="btn-secondary px-3 py-1.5 text-xs" onClick={onEdit}>
          Edit
        </button>
        <button type="button" className="btn-danger px-3 py-1.5 text-xs" onClick={onDelete}>
          Delete
        </button>
      </div>
      {runError && <p className="text-sm text-danger">{runError}</p>}
      <div>
        <h3 className="text-sm font-medium text-ink">History</h3>
        {detail.history.length === 0 ? (
          <p className="mt-2 text-sm text-ink-muted">This automation has not run yet.</p>
        ) : (
          <ul className="mt-2 space-y-3">
            {detail.history.map((run, index) => (
              <HistoryRow
                key={run.id}
                run={run}
                zone={zone}
                notification={detail.notification}
                previous={detail.history.slice(index + 1).find((item) => item.status === 'succeeded')?.result}
                previousNotified={detail.history.slice(index + 1).find((item) => item.status === 'succeeded')?.notification_sent ?? false}
                models={models}
              />
            ))}
          </ul>
        )}
      </div>
    </div>
  )
}

function HistoryRow({
  run,
  zone,
  notification,
  previous,
  previousNotified = false,
  models,
}: {
  run: AutomationRun
  zone: string
  notification: AutomationDetail['notification']
  previous?: string
  previousNotified?: boolean
  models: Model[]
}) {
  const notice = explainRun(notification, run, previous, previousNotified)
  const prose = resultProse(run.result)
  const modelName = models.find((model) => model.id === run.model_id)?.display_name || run.model_id
  return (
    <li className="rounded-lg bg-raised/50 p-3">
      <div className="flex items-start justify-between gap-3">
        <p className="text-sm font-medium text-ink">{statusLabel(run.status)}</p>
        <p className="text-xs text-ink-faint">{notice.title}</p>
      </div>
      <p className="mt-1 text-xs text-ink-faint">{runTiming(run, zone)}</p>
      {prose && <p className="mt-2 whitespace-pre-wrap text-sm text-ink-muted">{prose}</p>}
      {notice.detail && <p className="mt-1 text-sm text-ink-muted">{notice.detail}</p>}
      {run.error && <p className="mt-2 text-sm text-danger">{run.error}</p>}
      <details className="mt-2">
        <summary className="cursor-pointer text-xs text-ink-faint">Details</summary>
        <div className="mt-2 space-y-1 text-xs text-ink-faint">
          <p>Scheduled {clockDetail(run.occurrence_at, zone)}</p>
          {run.started_at && <p>Started {clockDetail(run.started_at, zone)}</p>}
          {run.finished_at && <p>Finished {clockDetail(run.finished_at, zone)}</p>}
          {modelName && <p>Model {modelName}</p>}
          {run.node_id && <p>Computer {run.node_id}</p>}
          {run.attempt > 1 && <p>Attempt {run.attempt}</p>}
        </div>
      </details>
    </li>
  )
}

function StatusPill({ item }: { item: Pick<Automation, 'enabled' | 'last_status' | 'consecutive_failures'> }) {
  const failed = item.last_status === 'failed' || item.consecutive_failures > 0
  const label = !item.enabled ? 'Paused' : failed ? 'Failed' : 'Enabled'
  const mark = !item.enabled ? 'Ⅱ' : failed ? '!' : '●'
  return (
    <span className={failed ? 'text-xs text-danger' : item.enabled ? 'text-xs text-success' : 'text-xs text-ink-faint'}>
      {mark} {label}
    </span>
  )
}

function matchesAutomation(item: Automation, query: string, filter: 'all' | 'active' | 'paused' | 'attention'): boolean {
  const needle = query.trim().toLowerCase()
  if (needle && !`${item.name} ${item.prompt} ${item.last_result ?? ''}`.toLowerCase().includes(needle)) return false
  if (filter === 'active') return item.enabled
  if (filter === 'paused') return !item.enabled
  if (filter === 'attention') return item.last_status === 'failed' || item.consecutive_failures > 0
  return true
}

function statusLabel(status: string | undefined): string {
  switch (status) {
    case 'succeeded':
      return 'Succeeded'
    case 'failed':
      return 'Failed'
    case 'retrying':
      return 'Retrying'
    case 'running':
    case 'claimed':
      return 'Running'
    default:
      return 'Not run yet'
  }
}
