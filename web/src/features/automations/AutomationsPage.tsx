import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate, useSearchParams } from 'react-router-dom'
import i18n from '@/i18n'
import { api, ApiError } from '@/lib/api'
import { subscribeEvents } from '@/lib/events'
import { readScreenshotLaunch } from '@/lib/screenshotMode'
import type { Automation, AutomationDetail, AutomationInput, AutomationRun, Model } from '@/types/api'
import { AutomationForm } from './AutomationForm'
import { clockDetail, compactWhen, explainRun, runTiming } from './display'
import { notificationLabel, resultProse, visibleTask, whenLabel } from './parseRequest'
import { RealmKicker } from '@/components/ui/Realm'
import { LoadError } from '@/components/ui/LoadError'
import { Skeleton } from '@/components/ui/Skeleton'
import { AutomationsIntro, IdeaGallery } from './Intro'
import type { IdeaId } from './ideas'
import { WebhookLink } from './WebhookLink'
import { BackgroundOffNotice } from './BackgroundOffNotice'
import { ShownError, shownError, type Shown } from '@/components/ui/ShownError'
import { useCapabilities } from '@/lib/capabilities'
import { matchesSearch } from '@/lib/search'

const screenshotSentence =
  'Every morning at 8:00 AM, check this product and tell me if the price is below $500.'

export function AutomationsPage() {
  const { t } = useTranslation('automations')
  const queryClient = useQueryClient()
  const [searchParams, setSearchParams] = useSearchParams()
  const [selectedID, setSelectedID] = useState<string | null>(() => searchParams.get('id'))
  const [creating, setCreating] = useState(false)
  const [editing, setEditing] = useState(false)
  const [formError, setFormError] = useState<Shown | null>(null)
  const [query, setQuery] = useState('')
  const [filter, setFilter] = useState<'all' | 'active' | 'paused' | 'attention'>('all')
  // A template to start the form from: an idea someone chose.
  const [seed, setSeed] = useState<IdeaId | null>(null)
  const [showIntro, setShowIntro] = useState(false)

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
    retry: false,
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
  // Names for an automation that runs after another (#204).
  // Device triggers show the device's name, for whoever can list devices (#586).
  const devicesQuery = useQuery({
    queryKey: ['devices'],
    queryFn: () => api.listDevices(),
    // Also for the Answer the door idea, offered with a doorbell connected (#1341).
    retry: false,
  })
  const names = Object.fromEntries([
    ...(devicesQuery.data ?? []).map((d) => [d.device, d.name]),
    ...(listQuery.data ?? []).map((item) => [item.id, item.name]),
  ])
  const detail = detailQuery.data
  const profiles = profilesQuery.data ?? []
  const tools = toolsQuery.data ?? []
  const models = modelsQuery.data ?? []
  const hasAutomations = (listQuery.data ?? []).length > 0
  // With none yet, the page explains automations and offers ideas instead of an empty list.
  const empty = !listQuery.isLoading && !listQuery.isError && !hasAutomations

  // Making automations may be turned off for the person (#589); the ones
  // they have still run and show here.
  const mayCreate = useCapabilities().allows('automations')

  function startNew(idea: IdeaId | null = null) {
    setSeed(idea)
    setCreating(true)
    setEditing(false)
    setSelectedID(null)
    setFormError(null)
  }

  useEffect(() => {
    if (!readScreenshotLaunch()?.enabled) return
    const compose = new URLSearchParams(window.location.search).get('compose') === '1'
    if (compose) {
      setCreating(true)
      return
    }
    if (!selectedID && items[0]) setSelectedID(items[0].id)
  }, [items, selectedID])

  // A connected camera links here with ?idea=door to set up answering the
  // door (#1341).
  const linkedIdea = searchParams.get('idea')
  useEffect(() => {
    if (linkedIdea !== 'door') return
    setSeed('door')
    setCreating(true)
    setEditing(false)
    setSelectedID(null)
    setSearchParams({}, { replace: true })
  }, [linkedIdea, setSearchParams])

  // A notification links here with ?id=…; open that automation.
  const linkedID = searchParams.get('id')
  useEffect(() => {
    if (!linkedID) return
    setSelectedID(linkedID)
    setCreating(false)
    setEditing(false)
    setSearchParams({}, { replace: true })
  }, [linkedID, setSearchParams])

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
      setFormError(null)
      setCreating(false)
      setEditing(false)
      if (saved?.id) setSelectedID(saved.id)
      refresh()
    },
    onError: (error) => setFormError(error instanceof ApiError ? shownError(error) : { error: t('page.saveFailed') }),
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
    <div className="page-fill gap-5 overflow-y-auto p-4 sm:p-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div className="min-w-0 max-w-2xl">
          <RealmKicker />
          <h1 className="page-title">{t('page.title')}</h1>
          <p className="page-subtitle mt-1">{t('page.description')}</p>
        </div>
        <div className="flex flex-wrap gap-2">
          {hasAutomations ? (
            <button
              type="button"
              className="btn-secondary"
              aria-expanded={showIntro}
              aria-controls="automations-intro"
              onClick={() => setShowIntro((open) => !open)}
            >
              {showIntro ? t('page.hideHowItWorks') : t('page.howItWorks')}
            </button>
          ) : null}
          {mayCreate ? (
            <button type="button" className="btn-primary" onClick={() => startNew()}>
              {t('page.new')}
            </button>
          ) : null}
        </div>
      </div>

      {hasAutomations ? <BackgroundOffNotice /> : null}

      {hasAutomations && showIntro ? (
        <div id="automations-intro" className="space-y-5">
          <AutomationsIntro />
          {mayCreate ? <IdeaGallery onUse={startNew} devices={devicesQuery.data ?? []} /> : null}
        </div>
      ) : null}

      {empty && !creating ? (
        <div className="space-y-6">
          <AutomationsIntro mascot />
          {mayCreate ? <IdeaGallery onUse={startNew} devices={devicesQuery.data ?? []} /> : <p className="text-sm text-ink-muted">{t('page.createOff')}</p>}
        </div>
      ) : (
      <div className="grid gap-5 lg:grid-cols-[minmax(0,1fr)_minmax(22rem,34rem)]">
        <section className="min-w-0 space-y-3">
          {empty ? (
            mayCreate ? <IdeaGallery onUse={startNew} compact devices={devicesQuery.data ?? []} /> : null
          ) : (
          <div className="flex flex-wrap items-center gap-2">
            <label className="search-field min-w-40 flex-1">
              <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth={1.5} strokeLinecap="round" aria-hidden>
                <circle cx="7" cy="7" r="4.5" />
                <path d="m10.5 10.5 3 3" />
              </svg>
              <input
                type="search"
                className="field"
                value={query}
                placeholder={t('page.search')}
                aria-label={t('page.searchLabel')}
                onChange={(event) => setQuery(event.target.value)}
              />
            </label>
            <div className="segmented" role="group" aria-label={t('page.searchLabel')}>
              {(['all', 'active', 'paused', 'attention'] as const).map((key) => (
                <button
                  key={key}
                  type="button"
                  className="segmented-item"
                  aria-pressed={filter === key}
                  onClick={() => setFilter(key)}
                >
                  {t(`page.filters.${key}`)}
                </button>
              ))}
            </div>
          </div>
          )}
          {listQuery.isLoading && <Skeleton label={t('page.loading')} />}
          {listQuery.isError && !listQuery.data && (
            <LoadError error={listQuery.error} onRetry={() => void listQuery.refetch()} retrying={listQuery.isFetching} />
          )}
          {hasAutomations && items.length === 0 && (
            <p className="text-sm text-ink-muted">{t('page.noMatches')}</p>
          )}
          <ul className="space-y-2">
            {items.map((item) => (
              <li key={item.id}>
                <button
                  type="button"
                  className={['selectable w-full', selectedID === item.id ? 'selectable-active' : ''].filter(Boolean).join(' ')}
                  aria-pressed={selectedID === item.id}
                  onClick={() => {
                    setSelectedID(item.id)
                    setCreating(false)
                    setEditing(false)
                    setFormError(null)
                  }}
                >
                  <div className="flex items-start justify-between gap-3">
                    <p className="selectable-title font-semibold text-ink">{item.name}</p>
                    <StatusPill item={item} />
                  </div>
                  <p className="mt-1 text-sm text-ink-muted">{whenLabel(item, names)}</p>
                  {resultProse(item.last_result) && (
                    <p className="mt-2 line-clamp-2 text-sm text-ink">{resultProse(item.last_result)}</p>
                  )}
                  <p className="mt-2 text-xs text-ink-faint">{cardTiming(item)}</p>
                </button>
              </li>
            ))}
          </ul>
        </section>
        <aside>
          {creating || editing ? (
            <AutomationForm
              key={editing ? selectedID ?? 'edit' : `new-${seed ?? ''}`}
              profiles={profiles}
              models={models}
              tools={tools}
              others={(listQuery.data ?? []).filter((item) => !(editing && item.id === selectedID))}
              initial={editing ? detail : null}
              seedDescription={creating && new URLSearchParams(window.location.search).get('compose') === '1' ? screenshotSentence : ''}
              seedIdea={creating ? seed : null}
              showIdeas={!empty}
              pending={save.isPending}
              error={formError}
              onCancel={() => {
                setSeed(null)
                setCreating(false)
                setEditing(false)
                setFormError(null)
              }}
              onSubmit={(input) => save.mutate(input)}
            />
          ) : detail ? (
            <Detail
              names={names}
              detail={detail}
              models={models}
              // Run now answers once the run starts (#204); it's running
              // until its history says it ended.
              running={runNow.isPending || (detail.history ?? []).some((run) => run.status === 'running' || run.status === 'claimed')}
              runError={runNow.error ? shownError(runNow.error) : null}
              onRun={() => runNow.mutate(detail.id)}
              onToggle={() => pause.mutate(detail)}
              onEdit={() => {
                setEditing(true)
                setFormError(null)
              }}
              onDelete={() => {
                if (window.confirm(t('page.confirmDelete', { name: detail.name }))) {
                  remove.mutate(detail.id)
                }
              }}
            />
          ) : selectedID && detailQuery.isLoading ? (
            <div className="card">
              <Skeleton label={t('page.loading')} shape="rows" count={4} />
            </div>
          ) : selectedID && detailQuery.isError ? (
            <div className="card">
              <LoadError compact error={detailQuery.error} onRetry={() => void detailQuery.refetch()} retrying={detailQuery.isFetching} />
            </div>
          ) : (
            <div className="card-outline text-sm text-ink-muted">{t('page.selectHint')}</div>
          )}
        </aside>
      </div>
      )}
    </div>
  )
}

function Detail({
  detail,
  names,
  models,
  running,
  runError,
  onRun,
  onToggle,
  onEdit,
  onDelete,
}: {
  detail: AutomationDetail
  names: Record<string, string>
  models: Model[]
  running: boolean
  runError: ReturnType<typeof shownError> | null
  onRun: () => void
  onToggle: () => void
  onEdit: () => void
  onDelete: () => void
}) {
  const { t } = useTranslation('automations')
  const zone = detail.schedule.time_zone
  // Older runs come a page at a time (#204); a new automation starts over.
  const [older, setOlder] = useState<{ id: string; runs: AutomationRun[]; more: boolean }>({ id: detail.id, runs: [], more: false })
  const pages = older.id === detail.id ? older : { id: detail.id, runs: [], more: false }
  const shown = new Set(detail.history.map((run) => run.id))
  const history = [...detail.history, ...pages.runs.filter((run) => !shown.has(run.id))]
  const more = pages.runs.length > 0 ? pages.more : Boolean(detail.history_more)
  const loadOlder = useMutation({
    mutationFn: () => api.listAutomationRuns(detail.id, history[history.length - 1].id),
    onSuccess: (page) => setOlder({ id: detail.id, runs: [...pages.runs, ...(page?.runs ?? [])], more: Boolean(page?.more) }),
  })
  // Clearing history keeps the automation and any run still going (#1335).
  const queryClient = useQueryClient()
  const [clearing, setClearing] = useState(false)
  const [withFiles, setWithFiles] = useState(false)
  const finished = history.filter((run) => run.status === 'succeeded' || run.status === 'failed')
  const refresh = () => {
    setOlder({ id: detail.id, runs: [], more: false })
    void queryClient.invalidateQueries({ queryKey: ['automation'] })
    void queryClient.invalidateQueries({ queryKey: ['automations'] })
  }
  const clear = useMutation({
    mutationFn: () => api.clearAutomationRuns(detail.id, withFiles),
    onSuccess: () => {
      setClearing(false)
      setWithFiles(false)
      refresh()
    },
  })
  const removeRun = useMutation({
    mutationFn: (runId: string) => api.deleteAutomationRun(detail.id, runId),
    onSuccess: (_, runId) => {
      setOlder({ ...pages, runs: pages.runs.filter((run) => run.id !== runId) })
      void queryClient.invalidateQueries({ queryKey: ['automation'] })
    },
  })
  return (
    <div className="card space-y-4">
      <div>
        <div className="flex items-start justify-between gap-3">
          <h2 className="font-display text-lg font-semibold text-ink">{detail.name}</h2>
          <StatusPill item={detail} />
        </div>
        <div className="mt-4 space-y-3 text-sm">
          <div>
            <p className="label-caps">{t('detail.schedule')}</p>
            <p className="text-ink">{whenLabel(detail, names)}</p>
            {detail.trigger?.kind && detail.last_checked_at ? (
              <p className="text-ink-muted">{t('detail.lastChecked', { when: compactWhen(detail.last_checked_at, zone) })}</p>
            ) : null}
            {/* A device, a webhook, or another automation starts it: nothing is checked on a schedule. */}
            {['device', 'webhook', 'after'].includes(detail.trigger?.kind ?? '') && !detail.next_run_at ? null : (
              <p className="text-ink-muted">
                {detail.trigger?.kind ? t('detail.nextCheck', { when: compactWhen(detail.next_run_at, zone) }) : t('detail.next', { when: compactWhen(detail.next_run_at, zone) })}
              </p>
            )}
          </div>
          {detail.trigger?.kind === 'webhook' ? <WebhookLink automationId={detail.id} hookSet={Boolean(detail.hook_set)} /> : null}
          {/* The camera it looks at and what it does at the door (#1341). */}
          {detail.cameras?.length || detail.say ? (
            <div>
              <p className="label-caps">{t('detail.door')}</p>
              {detail.cameras?.length ? (
                <p className="text-ink">{t('detail.looksAt', { cameras: detail.cameras.map((c) => names[c.device] ?? c.device).join(', ') })}</p>
              ) : null}
              {detail.say ? <p className="text-ink">{t('detail.says')}</p> : null}
              {detail.say?.listen ? <p className="text-ink-muted">{t('detail.talks')}</p> : null}
            </div>
          ) : null}
          <div>
            <p className="label-caps">{t('detail.task')}</p>
            <p className="whitespace-pre-wrap text-ink">{visibleTask(detail.prompt)}</p>
          </div>
          <div>
            <p className="label-caps">{t('detail.notification')}</p>
            <p className="text-ink">{notificationLabel(detail.notification)}</p>
            <p className="text-ink-muted">{detail.notification.mode === 'none' ? t('detail.storedHere') : t('detail.onThisComputer')}</p>
          </div>
        </div>
        {detail.last_error && (
          <ShownError
            error={detail.last_error}
            code={detail.last_error_code}
            details={detail.last_error_details}
            className="mt-3 text-sm text-danger"
          />
        )}
      </div>
      <div className="flex flex-wrap gap-2">
        <button type="button" className="btn-primary btn-sm" disabled={running} onClick={onRun}>
          {running ? t('detail.running') : t('detail.runNow')}
        </button>
        <button type="button" className="btn-secondary btn-sm" onClick={onToggle}>
          {detail.enabled ? t('detail.pause') : t('detail.resume')}
        </button>
        <button type="button" className="btn-secondary btn-sm" onClick={onEdit}>
          {t('detail.edit')}
        </button>
        <button type="button" className="btn-danger btn-sm" onClick={onDelete}>
          {t('detail.delete')}
        </button>
      </div>
      {runError && <ShownError {...runError} />}
      <div>
        <div className="flex items-center justify-between gap-3">
          <h3 className="label-caps">{t('detail.history')}</h3>
          {finished.length > 0 && !clearing ? (
            <button type="button" className="text-xs text-ink-muted hover:text-danger" onClick={() => setClearing(true)}>
              {t('detail.clearHistory')}
            </button>
          ) : null}
        </div>
        {clearing ? (
          <div role="group" aria-label={t('detail.clearHistory')} className="mt-2 space-y-2 rounded-lg border border-danger/40 p-3 text-sm">
            <p className="text-ink">{t('detail.clearConfirm')}</p>
            {detail.cameras?.length ? (
              <label className="flex items-center gap-2 text-ink-muted">
                <input type="checkbox" checked={withFiles} onChange={(e) => setWithFiles(e.target.checked)} />
                {t('detail.clearPictures')}
              </label>
            ) : null}
            <div className="flex flex-wrap gap-2">
              <button type="button" className="btn-danger btn-sm" disabled={clear.isPending} onClick={() => clear.mutate()}>
                {clear.isPending ? t('detail.clearing') : t('detail.clearAction')}
              </button>
              <button type="button" className="btn-secondary btn-sm" disabled={clear.isPending} onClick={() => setClearing(false)}>
                {t('detail.cancel')}
              </button>
            </div>
            {clear.isError ? <ShownError {...shownError(clear.error)} className="text-xs text-danger" /> : null}
          </div>
        ) : null}
        {history.length === 0 ? (
          <p className="mt-2 text-sm text-ink-muted">{t('detail.notRunYet')}</p>
        ) : (
          <ul className="mt-2 space-y-3">
            {history.map((run) => (
              <HistoryRow
                key={run.id}
                run={run}
                zone={zone}
                models={models}
                removing={removeRun.isPending && removeRun.variables === run.id}
                onRemove={() => removeRun.mutate(run.id)}
              />
            ))}
          </ul>
        )}
        {removeRun.isError ? <ShownError {...shownError(removeRun.error)} className="mt-2 text-xs text-danger" /> : null}
        {more ? (
          <button type="button" className="btn-secondary btn-sm mt-3" disabled={loadOlder.isPending} onClick={() => loadOlder.mutate()}>
            {loadOlder.isPending ? t('detail.loadingOlder') : t('detail.olderRuns')}
          </button>
        ) : null}
      </div>
    </div>
  )
}

function HistoryRow({
  run,
  zone,
  models,
  removing,
  onRemove,
}: {
  run: AutomationRun
  zone: string
  models: Model[]
  removing: boolean
  onRemove: () => void
}) {
  const { t } = useTranslation('automations')
  const navigate = useNavigate()
  const notice = explainRun(run)
  const prose = resultProse(run.result)
  // Reply to a result in a chat: the one it went to, or a new one (#204).
  const continueInChat = useMutation({
    mutationFn: () => api.continueAutomationRun(run.automation_id, run.id),
    onSuccess: (opened) => {
      if (opened) navigate(`/chat?c=${encodeURIComponent(opened.conversation_id)}`)
    },
  })
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
      {run.error && <ShownError error={run.error} code={run.error_code} details={run.error_details} className="mt-2 text-sm text-danger" />}
      {run.saved_file ? (
        <p className="mt-1 break-all text-xs text-ink-faint">{t('run.savedTo', { path: run.saved_file })}</p>
      ) : null}
      {run.status === 'succeeded' && prose ? (
        <div className="mt-2">
          <button type="button" className="btn-secondary btn-sm" disabled={continueInChat.isPending} onClick={() => continueInChat.mutate()}>
            {continueInChat.isPending ? t('run.openingChat') : t('run.continueInChat')}
          </button>
          {continueInChat.isError ? (
            <ShownError {...shownError(continueInChat.error)} className="mt-1 text-xs text-danger" />
          ) : null}
        </div>
      ) : null}
      <details className="mt-2">
        <summary className="cursor-pointer text-xs text-ink-faint">{t('run.details')}</summary>
        <div className="mt-2 space-y-1 text-xs text-ink-faint">
          <p>{t('run.scheduled', { when: clockDetail(run.occurrence_at, zone) })}</p>
          {run.started_at && <p>{t('run.started', { when: clockDetail(run.started_at, zone) })}</p>}
          {run.finished_at && <p>{t('run.finished', { when: clockDetail(run.finished_at, zone) })}</p>}
          {modelName && <p>{t('run.model', { model: modelName })}</p>}
          {run.node_id && <p>{t('run.computer', { computer: run.node_id })}</p>}
          {run.attempt > 1 && <p>{t('run.attempt', { n: run.attempt })}</p>}
          {run.status === 'succeeded' || run.status === 'failed' ? (
            <button type="button" className="mt-1 text-ink-muted hover:text-danger" disabled={removing} onClick={onRemove}>
              {removing ? t('run.removing') : t('run.remove')}
            </button>
          ) : null}
        </div>
      </details>
    </li>
  )
}

function StatusPill({ item }: { item: Pick<Automation, 'enabled' | 'last_status' | 'consecutive_failures'> }) {
  const { t } = useTranslation('automations')
  const failed = item.last_status === 'failed' || item.consecutive_failures > 0
  const label = !item.enabled ? t('pill.paused') : failed ? t('pill.failed') : t('pill.enabled')
  const mark = !item.enabled ? 'Ⅱ' : failed ? '!' : '●'
  return (
    <span
      className={[
        'status-chip shrink-0',
        failed ? 'bg-danger/15 text-danger' : item.enabled ? 'bg-success/15 text-success' : 'bg-raised text-ink-muted',
      ].join(' ')}
    >
      <span aria-hidden>{mark}</span> {label}
    </span>
  )
}

function matchesAutomation(item: Automation, query: string, filter: 'all' | 'active' | 'paused' | 'attention'): boolean {
  // Typos forgiven (#451).
  if (!matchesSearch(query, item.name, item.prompt, item.last_result)) return false
  if (filter === 'active') return item.enabled
  if (filter === 'paused') return !item.enabled
  if (filter === 'attention') return item.last_status === 'failed' || item.consecutive_failures > 0
  return true
}

function statusLabel(status: string | undefined): string {
  switch (status) {
    case 'succeeded':
      return i18n.t('automations:run.status.succeeded')
    case 'failed':
      return i18n.t('automations:run.status.failed')
    case 'retrying':
      return i18n.t('automations:run.status.retrying')
    case 'running':
    case 'claimed':
      return i18n.t('automations:run.status.running')
    default:
      return i18n.t('automations:run.status.notRun')
  }
}

/** A card's last and next run: "Never run" before the first, and no next
 * run for one a device, a webhook, or another automation starts (#1341). */
function cardTiming(item: Automation): string {
  const zone = item.schedule.time_zone
  const event = ['device', 'webhook', 'after'].includes(item.trigger?.kind ?? '') && !item.next_run_at
  const last = item.last_run_at ? compactWhen(item.last_run_at, zone) : ''
  if (event) return last ? i18n.t('automations:page.lastOnly', { last }) : i18n.t('automations:page.neverRun')
  if (!last) return `${i18n.t('automations:page.neverRun')} · ${i18n.t('automations:detail.next', { when: compactWhen(item.next_run_at, zone) })}`
  return i18n.t('automations:page.lastNext', { last, next: compactWhen(item.next_run_at, zone) })
}
