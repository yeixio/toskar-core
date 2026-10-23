import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { RealmKicker } from '@/components/ui/Realm'
import { LoadError } from '@/components/ui/LoadError'
import { Skeleton } from '@/components/ui/Skeleton'
import { ShownError, shownError } from '@/components/ui/ShownError'
import { formatDate, formatDateTime } from '@/i18n/format'
import { api } from '@/lib/api'
import { useRole } from '@/lib/role'
import { fitLabelText, fitTone, installAction } from '@/features/models/modelPresentation'
import type { Model, ModelFit, NewsItem } from '@/types/api'

/**
 * AI news (#750): new open models, runtime releases, and AI labs'
 * announcements, a few lines each, from toskar.ai. Core reads it when this
 * page asks, at most every six hours, and keeps a copy for offline. A model
 * in Toskar's catalog gets its fit here and, for an Admin, Install.
 */
export function NewsPage() {
  const { t } = useTranslation('news')
  const { role, atLeast } = useRole()
  const admin = role === undefined || atLeast('admin')
  const queryClient = useQueryClient()
  const news = useQuery({ queryKey: ['news'], queryFn: () => api.getNews(), retry: false })
  const items = news.data?.items ?? []
  const wantsModels = items.some((i) => i.model_id)
  const models = useQuery({ queryKey: ['models'], queryFn: () => api.getModels(), enabled: wantsModels })
  const fit = useQuery({ queryKey: ['models-fit'], queryFn: () => api.getModelsFit(), enabled: wantsModels && admin, retry: false })
  const turnOn = useMutation({
    mutationFn: () => api.updateSettings({ ai_news: true }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['settings'] })
      void queryClient.invalidateQueries({ queryKey: ['news'] })
    },
  })

  const byId = new Map((models.data ?? []).map((m) => [m.id, m]))
  // This computer's fit, the first answer; paired computers come after it.
  const fits = new Map((fit.data?.[0]?.fits ?? []).map((f) => [f.model_id, f]))
  const days: [string, NewsItem[]][] = []
  for (const item of items) {
    const last = days[days.length - 1]
    if (last && last[0] === item.date) last[1].push(item)
    else days.push([item.date, [item]])
  }

  return (
    <div className="page-fill gap-5 overflow-y-auto p-4 sm:p-6">
      <div className="min-w-0 max-w-2xl">
        <RealmKicker />
        <h1 className="page-title">{t('page.title')}</h1>
        <p className="page-subtitle mt-1">{t('page.description')}</p>
        <p className="mt-2 text-xs text-ink-faint">
          {t('page.byAI')}
          {news.data?.updated ? ` ${t('page.updated', { when: formatDateTime(news.data.updated) })}` : ''}
        </p>
      </div>

      {news.isLoading ? <Skeleton label={t('page.loading')} /> : null}
      {news.isError ? <LoadError error={news.error} onRetry={() => void news.refetch()} retrying={news.isFetching} /> : null}

      {news.data && !news.data.enabled ? (
        <section className="card max-w-2xl space-y-3">
          <h2 className="section-title">{t('off.title')}</h2>
          <p className="text-sm text-ink-muted">{admin ? t('off.admin') : t('off.member')}</p>
          {admin ? (
            <button type="button" className="btn-primary" disabled={turnOn.isPending} onClick={() => turnOn.mutate()}>
              {t('off.turnOn')}
            </button>
          ) : null}
          {turnOn.isError ? <ShownError {...shownError(turnOn.error)} className="text-sm text-danger" /> : null}
        </section>
      ) : null}

      {news.data?.enabled && news.data.stale && news.data.fetched_at ? (
        <p role="status" className="max-w-2xl rounded-xl border border-line px-4 py-2 text-sm text-ink-muted">
          {t('page.stale', { when: formatDateTime(news.data.fetched_at) })}
        </p>
      ) : null}

      {news.data?.enabled && items.length === 0 ? (
        <p className="max-w-2xl rounded-xl border border-line px-4 py-3 text-sm text-ink-muted">{t('page.empty')}</p>
      ) : null}

      {days.map(([date, list]) => (
        <section key={date} className="card max-w-3xl">
          <h2 className="section-title">{formatDate(`${date}T12:00:00Z`, { dateStyle: 'full' })}</h2>
          <ul className="mt-2 divide-y divide-line/60">
            {list.map((item) => (
              <NewsRow
                key={item.id}
                item={item}
                model={item.model_id ? byId.get(item.model_id) : undefined}
                fit={item.model_id ? fits.get(item.model_id) : undefined}
                admin={admin}
              />
            ))}
          </ul>
        </section>
      ))}
    </div>
  )
}

function NewsRow({ item, model, fit, admin }: { item: NewsItem; model?: Model; fit?: ModelFit; admin: boolean }) {
  const { t } = useTranslation('news')
  const queryClient = useQueryClient()
  const install = useMutation({
    mutationFn: (id: string) => api.installModel(id),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ['models'] }),
  })
  const action = installAction(fit)
  const installing = install.isSuccess || model?.status === 'downloading'
  return (
    <li className="py-3">
      <p className="flex flex-wrap items-center gap-2 text-xs">
        <span className="status-chip">{t(`kinds.${item.kind}`)}</span>
        <span className="text-ink-faint">{item.source.name}</span>
      </p>
      <a href={item.source.url} target="_blank" rel="noreferrer" className="mt-1.5 block font-medium text-ink hover:text-accent">
        {item.title}
      </a>
      <p className="mt-1 text-sm text-ink-muted">{item.summary}</p>
      {model ? (
        <div className="mt-2 flex flex-wrap items-center gap-2 text-xs">
          {model.installed ? (
            <span className="status-chip">{t('model.installed')}</span>
          ) : (
            <>
              {fit ? (
                <span className={['status-chip', fitTone(fit.label)].join(' ')}>
                  <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-current" aria-hidden />
                  {fitLabelText(fit.label)}
                </span>
              ) : null}
              {admin ? (
                installing ? (
                  <Link to="/models" className="text-accent hover:underline">
                    {t('model.installing')}
                  </Link>
                ) : (
                  <button
                    type="button"
                    className="btn-secondary btn-sm"
                    disabled={action.disabled || install.isPending}
                    onClick={() => install.mutate(model.id)}
                  >
                    {action.label}
                  </button>
                )
              ) : (
                <span className="text-ink-faint">{t('model.inCatalog')}</span>
              )}
            </>
          )}
          {install.isError ? <ShownError {...shownError(install.error)} className="text-danger" /> : null}
        </div>
      ) : null}
    </li>
  )
}
