import { useTranslation } from 'react-i18next'
import { useMutation, useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { LoadingSpinner } from '@/components/ui/LoadingSpinner'
import { api } from '@/lib/api'
import { formatBytes } from '@/lib/format'
import { formatPercent } from '@/i18n/format'
import type { BrowseModel, Model, ModelDownloadProgressPayload } from '@/types/api'
import { ShownError, shownOr } from '@/components/ui/ShownError'

export function BrowseAllPanel({
  installTarget,
  models,
  progress,
  failed = {},
  onClose,
  onInstalled,
}: {
  /** Resolved from the page-level Models for: selector. */
  installTarget?: string
  /** The model list, so a row shows its model downloading or installed. */
  models: Model[]
  /** Live download progress by model ID, from the page's events. */
  progress: Record<string, ModelDownloadProgressPayload>
  /** Why a download failed, by model ID, from the page's events. */
  failed?: Record<string, string>
  onClose: () => void
  onInstalled: () => void
}) {
  const { t } = useTranslation('models')
  const [query, setQuery] = useState('gguf instruct')
  const [submitted, setSubmitted] = useState('gguf instruct')

  const browseQuery = useQuery({
    queryKey: ['models-browse', submitted],
    queryFn: () => api.browseModels(submitted, 30),
    retry: false,
  })

  const installMutation = useMutation({
    mutationFn: (item: BrowseModel) =>
      api.installModelFromURL({
        source_url: item.source_url,
        display_name: item.display_name,
        id: item.id,
        filename: item.filename,
        size_bytes: item.size_bytes,
        parameters: item.parameters,
        variant: item.variant,
        tags: item.tags,
        node_id: installTarget,
      }),
    onSuccess: (result, item) => {
      setStarted((current) => ({ ...current, [item.id]: result.model_id || item.id }))
      onInstalled()
    },
  })
  // The models these rows started, by row: the download goes on in the
  // background after the request returns, so the row follows it.
  const [started, setStarted] = useState<Record<string, string>>({})

  // What a row's button shows: starting while the request is sent, then
  // the download as the model list and its progress events say, then
  // installed. A download that failed offers Install again.
  const rowState = (
    item: BrowseModel,
  ): { kind: 'idle' | 'starting' | 'downloading' | 'installed'; progress?: ModelDownloadProgressPayload; failed?: string } => {
    const modelId = started[item.id] ?? item.id
    const model = models.find((m) => m.id === modelId)
    const live = progress[modelId]
    if (installMutation.isPending && installMutation.variables?.id === item.id) return { kind: 'starting' }
    if (live || model?.status === 'downloading') return { kind: 'downloading', progress: live }
    if (model?.installed) return { kind: 'installed' }
    if (modelId in failed) return { kind: 'idle', failed: failed[modelId] || t('browse.installFailed') }
    if (started[item.id] && model?.status !== 'failed') return { kind: 'downloading' }
    return { kind: 'idle' }
  }

  return (
    <div className="space-y-4 rounded-xl border border-line bg-surface p-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="section-title">{t('browse.title')}</h2>
          <p className="mt-1 text-sm text-ink-muted">{t('browse.description')}</p>
        </div>
        <button type="button" className="btn-secondary text-sm" onClick={onClose}>
          {t('browse.close')}
        </button>
      </div>

      <form
        className="flex flex-wrap gap-2"
        onSubmit={(e) => {
          e.preventDefault()
          const next = query.trim() || 'gguf'
          // The same search again asks again, as after a failure.
          if (next === submitted) void browseQuery.refetch()
          setSubmitted(next)
        }}
      >
        <input
          className="field min-w-[220px] flex-1"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder={t('browse.placeholder')}
        />
        <button type="submit" className="btn-primary">
          {t('browse.search')}
        </button>
      </form>

      {browseQuery.isLoading && <LoadingSpinner label={t('browse.searching')} />}
      {browseQuery.isError && (
        <p className="flex flex-wrap items-center gap-3 text-sm text-danger" role="alert">
          {t('browse.failed')}
          <button
            type="button"
            className="font-medium text-primary hover:underline"
            disabled={browseQuery.isFetching}
            onClick={() => void browseQuery.refetch()}
          >
            {t('loadError.retry', { ns: 'common' })}
          </button>
        </p>
      )}
      {browseQuery.isSuccess && (browseQuery.data ?? []).length === 0 && <p className="text-sm text-ink-muted">{t('browse.none')}</p>}

      <ul className={(browseQuery.data ?? []).length > 0 ? 'divide-y divide-line rounded-xl border border-line' : 'hidden'}>
        {(browseQuery.data ?? []).map((item) => (
          <li
            key={item.id}
            className="flex min-w-0 flex-wrap items-center justify-between gap-3 px-4 py-3"
          >
            <div className="min-w-0">
              <p className="font-medium text-ink">{item.display_name}</p>
              <p className="mt-0.5 truncate text-xs text-ink-muted" title={item.repo_id}>
                {item.repo_id}
                {item.size_bytes ? ` · ${formatBytes(item.size_bytes)}` : ''}
                {item.variant ? ` · ${item.variant}` : ''}
              </p>
            </div>
            {(() => {
              const state = rowState(item)
              if (state.kind === 'installed') {
                // As an installed model's card shows it.
                return (
                  <span className="status-chip h-8 shrink-0 bg-success/15 px-2.5 text-success">
                    <svg viewBox="0 0 16 16" className="h-3.5 w-3.5" fill="none" stroke="currentColor" strokeWidth={1.8} strokeLinecap="round" strokeLinejoin="round" aria-hidden>
                      <path d="m3.5 8.5 3 3 6-7" />
                    </svg>
                    {t('install.installed')}
                  </span>
                )
              }
              const busy = state.kind !== 'idle'
              return (
                <div className="flex shrink-0 flex-col items-end gap-1">
                  <button
                    type="button"
                    className="btn-primary btn-sm"
                    disabled={busy}
                    aria-busy={busy}
                    onClick={() => installMutation.mutate(item)}
                  >
                    {state.kind === 'starting'
                      ? t('browse.starting')
                      : state.kind === 'downloading'
                        ? t('install.downloading')
                        : t('browse.install')}
                  </button>
                  {state.failed && (
                    <span className="max-w-xs text-end text-xs text-danger" role="alert">
                      {state.failed}
                    </span>
                  )}
                  {state.progress && (
                    <span className="text-xs text-ink-muted">
                      {t('install.progress', {
                        percent: formatPercent(state.progress.percent / 100),
                        done: formatBytes(state.progress.bytes_downloaded),
                        total: formatBytes(state.progress.bytes_total),
                      })}
                    </span>
                  )}
                </div>
              )
            })()}
          </li>
        ))}
      </ul>
      {installMutation.isError && <ShownError {...shownOr(installMutation.error, t('browse.installFailed'))} />}
    </div>
  )
}
