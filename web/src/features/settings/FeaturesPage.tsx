import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { api } from '@/lib/api'
import { formatBytes } from '@/lib/format'
import { LoadError } from '@/components/ui/LoadError'
import { Skeleton } from '@/components/ui/Skeleton'
import { ShownError, shownError } from '@/components/ui/ShownError'
import type { Feature, FeatureNeed } from '@/types/api'

const queryKey = ['features']

/** Where a need nothing here can install is set up instead. */
const elsewhere: Partial<Record<FeatureNeed['need'], string>> = {
  camera: '/settings/tools',
  trainer: '/settings/computers',
}

/**
 * Features (#1355): each feature, Ready or Needs setup, and one press that
 * installs everything it still needs. When nothing that fits this computer
 * exists, it says what would.
 */
export function FeaturesPage() {
  const { t } = useTranslation('settings')
  const list = useQuery({
    queryKey,
    queryFn: () => api.listFeatures(),
    retry: false,
    // While something installs, check how it's going.
    refetchInterval: (q) => ((q.state.data ?? []).some((f) => f.installing) ? 3000 : false),
  })
  return (
    <div className="mx-auto w-full max-w-2xl min-w-0 space-y-6">
      <header className="page-header">
        <h1 className="page-title">{t('sections.features')}</h1>
        <p className="page-subtitle">{t('features.intro')}</p>
      </header>
      {list.isLoading ? (
        <div className="card">
          <Skeleton label={t('features.loading')} shape="rows" count={5} />
        </div>
      ) : list.isError ? (
        <div className="card">
          <LoadError compact error={list.error} onRetry={() => void list.refetch()} retrying={list.isFetching} />
        </div>
      ) : (
        <ul className="card divide-y divide-line p-0">
          {(list.data ?? []).map((feature) => (
            <FeatureRow key={feature.id} feature={feature} />
          ))}
        </ul>
      )}
    </div>
  )
}

function FeatureRow({ feature }: { feature: Feature }) {
  const { t } = useTranslation('settings')
  const queryClient = useQueryClient()
  const setUp = useMutation({
    mutationFn: () => api.setUpFeature(feature.id),
    onSettled: () => void queryClient.invalidateQueries({ queryKey }),
  })
  const missing = feature.needs.filter((n) => !n.ready)
  const installable = missing.filter((n) => n.install && !n.installing)
  const installing = feature.installing || setUp.isPending
  const percents = missing.filter((n) => n.installing && n.percent).map((n) => n.percent!)
  const percent = percents.length ? Math.min(...percents) : undefined
  const status = feature.ready ? 'ready' : installing ? 'installing' : 'needsSetup'
  return (
    <li className="space-y-2 p-4">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <h2 className="font-medium text-ink">{t(`features.items.${feature.id}.title`)}</h2>
          <p className="text-sm text-ink-muted">{t(`features.items.${feature.id}.what`)}</p>
        </div>
        <span
          className={[
            'status-chip shrink-0',
            status === 'ready' ? 'bg-success/15 text-success' : status === 'installing' ? 'bg-primary/15 text-primary' : 'bg-warning/15 text-warning',
          ].join(' ')}
        >
          {t(`features.status.${status}`)}
        </span>
      </div>
      {feature.ready ? null : (
        <>
          <ul className="space-y-1 text-sm">
            {missing.map((need) => (
              <NeedLine key={need.need} need={need} />
            ))}
          </ul>
          {installing ? (
            <div className="space-y-1" role="status">
              <p className="text-xs text-ink-muted">
                {percent !== undefined ? t('features.settingUpPercent', { percent }) : t('features.settingUp')}
              </p>
              <div
                className="h-2 overflow-hidden rounded-full bg-raised"
                role="progressbar"
                aria-label={t('features.settingUp')}
                aria-valuenow={percent}
                aria-valuemin={0}
                aria-valuemax={100}
              >
                <div
                  className={['h-full bg-primary transition-[width] duration-500', percent === undefined ? 'w-1/3 animate-pulse' : ''].join(' ')}
                  style={percent === undefined ? undefined : { width: `${percent}%` }}
                />
              </div>
            </div>
          ) : installable.length > 0 ? (
            <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
              <button type="button" className="btn-primary btn-sm" onClick={() => setUp.mutate()}>
                {feature.setup_bytes > 0
                  ? t('features.setUpSize', { size: formatBytes(feature.setup_bytes) })
                  : t('features.setUp')}
              </button>
              {installable.length < missing.length ? (
                <span className="text-xs text-ink-muted">{t('features.partly')}</span>
              ) : null}
            </div>
          ) : null}
          {setUp.isError ? <ShownError {...shownError(setUp.error)} className="text-xs text-danger" /> : null}
        </>
      )}
    </li>
  )
}

function NeedLine({ need }: { need: FeatureNeed }) {
  const { t } = useTranslation('settings')
  const what = t(`features.needs.${need.need}`)
  if (need.install) {
    const where = need.node_name ? t('features.onComputer', { computer: need.node_name }) : ''
    return (
      <li className="text-ink">
        {t('features.willInstall', { what, name: need.name ?? '', where })}
        {need.size_bytes ? <span className="text-ink-faint"> · {formatBytes(need.size_bytes)}</span> : null}
      </li>
    )
  }
  const link = elsewhere[need.need]
  return (
    <li className="text-ink-muted">
      {need.detail ? t('features.cannot', { what, why: need.detail }) : t(`features.none.${need.need}`)}{' '}
      {link ? (
        <Link to={link} className="text-primary underline">
          {t(`features.go.${need.need}`)}
        </Link>
      ) : need.need === 'chat' || need.need === 'tools' || need.need === 'vision' || need.need === 'embedding' ? (
        <Link to="/models" className="text-primary underline">
          {t('features.go.models')}
        </Link>
      ) : null}
    </li>
  )
}
