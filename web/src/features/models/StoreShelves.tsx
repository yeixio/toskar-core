import { useMutation, useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { formatDate, formatNumber, formatPercent } from '@/i18n/format'
import { api } from '@/lib/api'
import { formatBytes } from '@/lib/format'
import { searchItems } from '@/lib/search'
import { useDialog } from '@/lib/useDialog'
import type { Model, ModelDownloadProgressPayload, StoreModel } from '@/types/api'
import { fitLabelText, fitTone } from './modelPresentation'

/** Shelves in the order they're shown; one this build doesn't know is left out. */
const SHELF_ORDER = ['picks', 'popular', 'top_rated', 'new', 'everyday', 'code', 'sees', 'small', 'tested']

function compact(value: number): string {
  return formatNumber(value, { notation: 'compact', maximumFractionDigits: 1 })
}

type Row = { kind: 'idle' | 'starting' | 'downloading' | 'installed'; progress?: ModelDownloadProgressPayload }

/** The store's shelves (#593): Hugging Face's rankings for this computer, and what's been tested. */
export function StoreShelves({
  models,
  progress,
  search,
  installTarget,
  isOtherComputer,
  onInstallTested,
  onInstalled,
  onBrowseAll,
}: {
  models: Model[]
  progress: Record<string, ModelDownloadProgressPayload>
  search: string
  installTarget?: string
  /** Models for another computer: the shelves' fit is this one's. */
  isOtherComputer: boolean
  onInstallTested: (id: string) => void
  onInstalled: () => void
  onBrowseAll: () => void
}) {
  const { t } = useTranslation('models')
  const [open, setOpen] = useState<StoreModel | null>(null)
  // The model each row started, since the download goes on after the request.
  const [started, setStarted] = useState<Record<string, string>>({})
  const store = useQuery({
    queryKey: ['models-store'],
    queryFn: () => api.getModelStore(),
    retry: false,
    staleTime: 10 * 60_000,
  })
  const install = useMutation({
    mutationFn: (m: StoreModel) =>
      api.installModelFromURL({
        source_url: m.source_url,
        display_name: m.display_name,
        id: m.id,
        filename: m.filename,
        size_bytes: m.size_bytes,
        parameters: m.parameters,
        variant: m.variant,
        tags: m.tags,
        node_id: installTarget,
      }),
    onSuccess: (result, m) => {
      setStarted((current) => ({ ...current, [m.id]: result?.model_id || m.id }))
      onInstalled()
    },
  })

  const rowOf = (m: StoreModel): Row => {
    const id = m.catalog_id || started[m.id] || m.id
    const model = models.find((x) => x.id === id)
    const live = progress[id]
    if (live || model?.status === 'downloading') return { kind: 'downloading', progress: live }
    if (model?.installed || (m.installed && !isOtherComputer)) return { kind: 'installed' }
    if (install.isPending && install.variables?.id === m.id) return { kind: 'starting' }
    return { kind: 'idle' }
  }
  const installOne = (m: StoreModel) => {
    if (m.catalog_id) onInstallTested(m.catalog_id)
    else install.mutate(m)
  }

  const shelves = SHELF_ORDER.flatMap((id) => {
    const shelf = store.data?.shelves.find((s) => s.id === id)
    if (!shelf) return []
    const list = search ? searchItems(shelf.models, search, (m) => [m.display_name, m.repo_id, ...(m.tags ?? [])]) : shelf.models
    return list.length > 0 ? [{ id, models: list }] : []
  })

  if (store.isLoading) return <p className="text-sm text-ink-muted">{t('store.loading')}</p>

  return (
    <div className="space-y-8">
      {store.data && !store.data.live ? <p className="text-sm text-ink-muted">{t('store.offline')}</p> : null}
      {store.data?.live ? <p className="text-xs text-ink-faint">{t('store.source')}</p> : null}
      {shelves.map((shelf) => (
        <section key={shelf.id} className="space-y-3" aria-labelledby={`shelf-${shelf.id}`}>
          <div className="flex flex-wrap items-end justify-between gap-3">
            <div>
              <h2 id={`shelf-${shelf.id}`} className="section-title">
                {t(`store.shelves.${shelf.id}.title`)}
              </h2>
              <p className="mt-1 text-sm text-ink-muted">{t(`store.shelves.${shelf.id}.hint`)}</p>
            </div>
            {shelf.id === 'picks' ? (
              <button type="button" className="btn-secondary gap-1.5" onClick={onBrowseAll}>
                {t('discover.browseAll')}{' '}
                <span className="inline-block rtl:-scale-x-100" aria-hidden>
                  →
                </span>
              </button>
            ) : null}
          </div>
          <ul className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
            {shelf.models.map((m) => (
              <li key={`${shelf.id}-${m.id}`}>
                <StoreCard
                  model={m}
                  row={rowOf(m)}
                  showFit={!isOtherComputer}
                  badge={shelf.id === 'picks' && m.catalog_id ? t('store.bestOverall') : undefined}
                  onOpen={() => setOpen(m)}
                  onInstall={() => installOne(m)}
                />
              </li>
            ))}
          </ul>
        </section>
      ))}
      {!search ? (
        <section className="space-y-3" aria-labelledby="shelf-media">
          <div>
            <h2 id="shelf-media" className="section-title">
              {t('store.shelves.media.title')}
            </h2>
            <p className="mt-1 text-sm text-ink-muted">{t('store.shelves.media.hint')}</p>
          </div>
          <Link to="/tools" className="btn-secondary inline-flex">
            {t('store.shelves.media.action')}
          </Link>
        </section>
      ) : null}
      {install.isError ? <p className="text-sm text-danger">{t('store.installFailed')}</p> : null}
      {open ? (
        <StoreDetails
          model={open}
          row={rowOf(open)}
          showFit={!isOtherComputer}
          onClose={() => setOpen(null)}
          onInstall={() => installOne(open)}
        />
      ) : null}
    </div>
  )
}

function purposes(m: StoreModel): string[] {
  const out: string[] = []
  for (const tag of m.tags ?? []) {
    if (tag === 'coding' || tag === 'vision' || tag === 'reasoning' || tag === 'general') out.push(tag)
  }
  return out
}

function InstallButton({ row, onInstall }: { row: Row; onInstall: () => void }) {
  const { t } = useTranslation('models')
  if (row.kind === 'installed') return <span className="text-sm font-medium text-success">{t('install.installed')}</span>
  if (row.kind === 'downloading') {
    return (
      <span className="text-xs text-ink-muted tabular-nums">
        {row.progress ? formatPercent(row.progress.percent / 100) : t('install.downloading')}
      </span>
    )
  }
  return (
    <button type="button" className="btn-primary btn-sm" disabled={row.kind === 'starting'} onClick={onInstall}>
      {t('store.install')}
    </button>
  )
}

function Stats({ model }: { model: StoreModel }) {
  const { t } = useTranslation('models')
  return (
    <p className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-ink-muted tabular-nums">
      {model.likes ? <span title={t('store.likes')}>♥ {compact(model.likes)}</span> : null}
      {model.downloads ? <span title={t('store.downloads')}>↓ {compact(model.downloads)}</span> : null}
      {model.size_bytes ? <span>{formatBytes(model.size_bytes)}</span> : null}
    </p>
  )
}

function FitChip({ model }: { model: StoreModel }) {
  const { t } = useTranslation('models')
  return (
    <span className={['status-chip', fitTone(model.fit?.label)].join(' ')}>
      <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-current" aria-hidden />
      {fitLabelText(model.fit?.label) || t('fit.unknown')}
    </span>
  )
}

function StoreCard({
  model,
  row,
  showFit,
  badge,
  onOpen,
  onInstall,
}: {
  model: StoreModel
  row: Row
  showFit: boolean
  badge?: string
  onOpen: () => void
  onInstall: () => void
}) {
  const { t } = useTranslation('models')
  return (
    <article className="card flex h-full min-w-0 flex-col gap-2">
      {badge ? <span className="badge-preferred self-start">★ {badge}</span> : null}
      <h3 className="min-w-0 break-words font-display text-base font-semibold tracking-tight text-ink">
        <button type="button" className="text-start hover:underline" onClick={onOpen}>
          {model.display_name}
        </button>
      </h3>
      <p className="flex flex-wrap gap-1.5">
        {purposes(model).map((tag) => (
          <span key={tag} className="status-chip bg-raised/80 text-ink-muted">
            {t(`store.purpose.${tag}`)}
          </span>
        ))}
        {model.catalog_id ? <span className="status-chip bg-success/10 text-success">{t('store.tested')}</span> : null}
      </p>
      <Stats model={model} />
      <div className="mt-auto flex items-center justify-between gap-2 pt-1">
        {showFit ? <FitChip model={model} /> : <span />}
        <InstallButton row={row} onInstall={onInstall} />
      </div>
    </article>
  )
}

function StoreDetails({
  model,
  row,
  showFit,
  onClose,
  onInstall,
}: {
  model: StoreModel
  row: Row
  showFit: boolean
  onClose: () => void
  onInstall: () => void
}) {
  const { t } = useTranslation('models')
  const ref = useDialog(true, onClose)
  return (
    <div className="fixed inset-0 z-50 flex items-end justify-center scrim p-4 sm:items-center" role="presentation" onClick={onClose}>
      <div
        ref={ref}
        className="card w-full max-w-lg space-y-3 shadow-panel"
        role="dialog"
        aria-modal="true"
        aria-labelledby="store-details-title"
        onClick={(event) => event.stopPropagation()}
      >
        <h2 id="store-details-title" className="break-words font-display text-lg font-semibold text-ink">
          {model.display_name}
        </h2>
        {model.summary ? <p className="text-sm text-ink-muted">{model.summary}</p> : null}
        <Stats model={model} />
        <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
          {showFit ? (
            <>
              <dt className="text-ink-muted">{t('store.details.fit')}</dt>
              <dd>
                <FitChip model={model} />
                {model.fit?.reason ? <span className="mt-1 block text-xs text-ink-muted">{model.fit.reason}</span> : null}
              </dd>
            </>
          ) : null}
          <dt className="text-ink-muted">{t('store.details.license')}</dt>
          <dd>{model.license || t('store.details.licenseUnknown')}</dd>
          {model.created_at ? (
            <>
              <dt className="text-ink-muted">{t('store.details.added')}</dt>
              <dd>{formatDate(model.created_at)}</dd>
            </>
          ) : null}
          <dt className="text-ink-muted">{t('store.details.from')}</dt>
          <dd className="break-all">{model.repo_id}</dd>
        </dl>
        <p className="text-sm text-ink-muted">{t(model.catalog_id ? 'store.details.tested' : 'store.details.untested')}</p>
        <div className="flex flex-wrap items-center justify-between gap-2">
          {model.details_url ? (
            <a className="btn-secondary btn-sm" href={model.details_url} target="_blank" rel="noreferrer noopener">
              {t('store.details.link')}
            </a>
          ) : (
            <span />
          )}
          <div className="flex items-center gap-2">
            <button type="button" className="btn-secondary btn-sm" data-autofocus onClick={onClose}>
              {t('store.details.close')}
            </button>
            <InstallButton row={row} onInstall={onInstall} />
          </div>
        </div>
      </div>
    </div>
  )
}
