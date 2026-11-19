import { useEffect, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { api } from '@/lib/api'
import { subscribeEvents } from '@/lib/events'
import { formatBytes } from '@/lib/format'
import { ShownError, shownError, type Shown } from '@/components/ui/ShownError'
import type { ModelDownloadProgressPayload, ModelNeed } from '@/types/api'

/**
 * The models voice mode and the doorbell still need on this computer (#667):
 * a chat model to answer with, and one that can see a camera's picture. Each
 * names the one that fits and installs it with one press, so an Admin learns
 * what's missing while setting up, not when someone is at the door.
 */
export function ModelNeeds({ needs }: { needs: ModelNeed[] }) {
  if (needs.length === 0) return null
  return (
    <ul className="space-y-2">
      {needs.map((need) => (
        <NeedRow key={need.ability} need={need} />
      ))}
    </ul>
  )
}

function NeedRow({ need }: { need: ModelNeed }) {
  const { t } = useTranslation('settings')
  const queryClient = useQueryClient()
  const [started, setStarted] = useState(false)
  const [progress, setProgress] = useState(0)
  const [error, setError] = useState<Shown | null>(null)
  const models = useQuery({
    queryKey: ['models'],
    queryFn: () => api.getModels(),
    enabled: started,
    retry: false,
    refetchInterval: (q) => (started && !q.state.data?.find((m) => m.id === need.model)?.installed ? 3000 : false),
  })
  const installed = started && !!models.data?.find((m) => m.id === need.model)?.installed

  useEffect(() => {
    if (!started || !need.model) return
    return subscribeEvents({
      onEvent: (event) => {
        const p = event.payload as (Partial<ModelDownloadProgressPayload> & { model_id?: string; error?: string; error_code?: string }) | undefined
        if (!p || p.model_id !== need.model) return
        if (event.type === 'model.download.progress') setProgress(Math.round(p.percent ?? 0))
        if (event.type === 'model.download.failed') {
          setStarted(false)
          setError(p.error ? { error: p.error, code: p.error_code } : { error: t('voice.failed') })
        }
      },
    })
  }, [started, need.model, t])

  // Once it's here, the Voice settings no longer list it.
  useEffect(() => {
    if (installed) void queryClient.invalidateQueries({ queryKey: ['speech-voices'] })
  }, [installed, queryClient])

  return (
    <li className="rounded-lg border border-line p-3 text-sm space-y-2">
      <p className="text-ink">{t(`voice.needs.${need.ability}`)}</p>
      {!need.model ? (
        <p className="text-xs text-ink-muted">
          {t('voice.needs.none')}{' '}
          <Link to="/models" className="underline">
            {t('voice.needs.models')}
          </Link>
        </p>
      ) : installed ? (
        <p className="text-xs text-ink-muted" role="status">
          {t('voice.needs.ready', { name: need.name })}
        </p>
      ) : started ? (
        <div className="space-y-1" role="status">
          <p className="text-xs text-ink-muted">{t('voice.needs.downloading', { name: need.name, percent: progress })}</p>
          <div className="h-2 overflow-hidden rounded-full bg-raised" role="progressbar" aria-valuenow={progress} aria-valuemin={0} aria-valuemax={100}>
            <div className="h-full bg-primary transition-[width] duration-500" style={{ width: `${progress}%` }} />
          </div>
        </div>
      ) : (
        <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
          <span className="min-w-[min(100%,14rem)] flex-1 text-xs text-ink-muted">
            {t('voice.needs.offer', { name: need.name, size: t('models:size.download', { size: formatBytes(need.size_bytes) }) })}
          </span>
          <button
            type="button"
            className="btn-primary px-3 py-1.5 text-sm"
            onClick={async () => {
              setError(null)
              try {
                await api.installModel(need.model!)
                setStarted(true)
              } catch (err) {
                setError(shownError(err))
              }
            }}
          >
            {t('voice.needs.install', { name: need.name })}
          </button>
        </div>
      )}
      {error ? <ShownError {...error} className="text-xs text-danger" /> : null}
    </li>
  )
}
