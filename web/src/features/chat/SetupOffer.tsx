import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { formatSize } from '@/i18n/format'
import { api, type MediaKind } from '@/lib/api'
import { subscribeEvents } from '@/lib/events'
import type { ModelDownloadProgressPayload, SetupOffer } from '@/types/api'
import { ShownError, shownError, type Shown } from '@/components/ui/ShownError'

function gb(bytes: number): string {
  return formatSize(bytes, { base: 1000 })
}

/**
 * Offers to install what a request needed (Gungnir §29): what it is, its
 * size, and where it runs. Once it is installed from here, the request is
 * sent again so it is finished.
 */
// What a tight model is short of, with what it needs and what the computer
// has (#550); an older computer sends neither, and gets the plain words.
const needsKeys = {
  memory: { here: 'setup.needsMemory', on: 'setup.needsMemoryOn' },
  graphics: { here: 'setup.needsGraphics', on: 'setup.needsGraphicsOn' },
  shared: { here: 'setup.needsShared', on: 'setup.needsSharedOn' },
} as const

export function SetupOfferCard({ offer, onContinue }: { offer: SetupOffer; onContinue: (text: string, attachments?: string[]) => void }) {
  if (offer.ability === 'vision') return <VisionOfferCard offer={offer} onContinue={onContinue} />
  return <MediaOfferCard offer={offer} onContinue={onContinue} />
}

function MediaOfferCard({ offer, onContinue }: { offer: SetupOffer; onContinue: (text: string) => void }) {
  const { t } = useTranslation('chat')
  const queryClient = useQueryClient()
  const [started, setStarted] = useState(false)
  const [error, setError] = useState<Shown | null>(null)
  const continued = useRef(false)
  // The setup each installable ability uses.
  const kind: MediaKind | null = offer.ability === 'image_generation' ? 'images' : offer.ability === 'video_generation' ? 'video' : null
  // A paired computer's setup is started and followed there (#153).
  const nodeId = offer.remote ? offer.node_id : undefined
  const statusKey = nodeId ? [kind ?? 'none', 'setup', nodeId] : [kind ?? 'none', 'setup']
  const statusQuery = useQuery({
    queryKey: statusKey,
    queryFn: () => api.getMediaSetup(kind!, nodeId),
    enabled: kind !== null,
    retry: false,
    refetchInterval: (query) => (query.state.data?.job?.running ? 1000 : false),
  })
  const status = statusQuery.data
  const job = status?.job
  const ready = !!status?.ready

  // Installed from this card: finish the request, once.
  useEffect(() => {
    if (!ready || !started || continued.current) return
    continued.current = true
    void queryClient.invalidateQueries({ queryKey: ['tools'] })
    if (offer.request) onContinue(offer.request)
  }, [ready, started, offer.request, onContinue, queryClient])

  if (!kind || !status) return null

  const where = offer.node_name ? `${offer.node_name} (${t('setup.thisComputer')})` : t('setup.thisComputer')
  const details =
    offer.remote && offer.node_name
      ? offer.free_bytes
        ? t('setup.detailsRemote', { name: offer.name, size: gb(offer.size_bytes), node: offer.node_name, free: gb(offer.free_bytes) })
        : t('setup.details', { name: offer.name, size: gb(offer.size_bytes), node: offer.node_name })
      : t('setup.details', { name: offer.name, size: gb(offer.size_bytes), node: where })
  // Slow or tight is said of the computer that would make them.
  const there = offer.remote ? offer.node_name : undefined
  const percent = job && job.total_bytes > 0 ? Math.round((job.done_bytes / job.total_bytes) * 100) : 0
  const failed: Shown | undefined = error ?? (job && !job.running && job.error ? { error: job.error } : undefined)

  return (
    <div className="mt-3 space-y-2 rounded-xl border border-line/70 bg-surface px-3 py-2.5 text-sm">
      {/* Said before it's set up: it will be slow here, or may fail. */}
      {!ready && offer.tight_memory ? (
        <p className="text-xs text-warning">
          {offer.short && offer.need_bytes
            ? t(there ? needsKeys[offer.short].on : needsKeys[offer.short].here, {
                need: gb(offer.need_bytes),
                have: gb(offer.have_bytes ?? 0),
                node: there,
              })
            : there
              ? t('setup.tightMemoryOn', { node: there })
              : t('setup.tightMemory')}
        </p>
      ) : null}
      {!ready && offer.slow ? (
        <p className="text-xs text-ink-muted">
          {there
            ? t(kind === 'video' ? 'setup.slowVideoOn' : 'setup.slowImageOn', { node: there })
            : t(kind === 'video' ? 'setup.slowVideo' : 'setup.slowImage')}
        </p>
      ) : null}
      {ready ? (
        <div className="flex flex-wrap items-center gap-2">
          <span>{started && offer.request ? t('setup.continuing') : t('setup.ready', { label: offer.label })}</span>
          {!started && offer.request ? (
            <button type="button" className="btn-secondary px-3 py-1 text-xs" onClick={() => onContinue(offer.request!)}>
              {t('setup.continue')}
            </button>
          ) : null}
        </div>
      ) : job?.running ? (
        <div className="space-y-1.5">
          <div className="flex items-center justify-between gap-2 text-xs text-ink-muted">
            <span>
              {job.stage === 'program'
                ? t('setup.installing')
                : t('setup.downloading', { done: gb(job.done_bytes), total: gb(job.total_bytes) })}
            </span>
            <button
              type="button"
              className="text-xs text-ink-faint underline-offset-2 hover:text-ink hover:underline"
              onClick={() => void api.cancelMediaSetup(kind, nodeId).then(() => statusQuery.refetch())}
            >
              {t('setup.stop')}
            </button>
          </div>
          <div className="h-2 overflow-hidden rounded-full bg-raised" role="progressbar" aria-valuenow={percent} aria-valuemin={0} aria-valuemax={100}>
            <div className="h-full bg-primary transition-[width] duration-500" style={{ width: `${percent}%` }} />
          </div>
        </div>
      ) : (
        <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
          <span className="min-w-0 flex-1 text-xs text-ink-muted">{details}</span>
          <button
            type="button"
            className="btn-primary px-3 py-1.5 text-xs"
            onClick={async () => {
              setError(null)
              try {
                // The warning above was shown before Set up, so a tight
                // computer has been agreed to (#550).
                const next = await api.startMediaSetup(kind, offer.option, nodeId, { acceptTight: !!offer.tight_memory })
                setStarted(true)
                if (next) queryClient.setQueryData(statusKey, next)
                else void statusQuery.refetch()
              } catch (err) {
                setError(shownError(err))
              }
            }}
          >
            {t('setup.install')}
          </button>
        </div>
      )}
      {failed && !ready ? <ShownError {...failed} className="text-xs text-danger" /> : null}
    </div>
  )
}

/**
 * Offers a model that can see pictures, for a question sent with pictures
 * no installed model can see (#529). Once it's installed from here, the
 * question is sent again with its pictures.
 */
function VisionOfferCard({ offer, onContinue }: { offer: SetupOffer; onContinue: (text: string, attachments?: string[]) => void }) {
  const { t } = useTranslation('chat')
  const queryClient = useQueryClient()
  const [started, setStarted] = useState(false)
  const [progress, setProgress] = useState<number | null>(null)
  const [error, setError] = useState<ReturnType<typeof shownError> | null>(null)
  const continued = useRef(false)
  // While its download runs, the list is checked too, in case the end
  // came before the events were followed.
  const modelsQuery = useQuery({
    queryKey: ['models'],
    queryFn: () => api.getModels(),
    retry: false,
    refetchInterval: (query) => (started && !query.state.data?.find((m) => m.id === offer.option)?.installed ? 3000 : false),
  })
  const installed = !!modelsQuery.data?.find((m) => m.id === offer.option)?.installed

  // Follow the download this card started.
  useEffect(() => {
    if (!started) return
    return subscribeEvents({
      onEvent: (event) => {
        const payload = event.payload as
          | (Partial<ModelDownloadProgressPayload> & { model_id?: string; error?: string; error_code?: string; error_details?: Record<string, unknown> })
          | undefined
        if (payload?.model_id !== offer.option) return
        if (event.type === 'model.download.progress') setProgress(Math.round(payload.percent ?? 0))
        if (event.type === 'model.download.failed') {
          setProgress(null)
          setError(payload.error ? { error: payload.error, code: payload.error_code, details: payload.error_details } : { error: t('send.downloadFailed') })
        }
        if (event.type === 'model.download.completed') void queryClient.invalidateQueries({ queryKey: ['models'] })
      },
    })
  }, [started, offer.option, queryClient, t])

  // Installed from this card: answer the question, with its pictures, once.
  useEffect(() => {
    if (!installed || !started || continued.current) return
    continued.current = true
    if (offer.request) onContinue(offer.request, offer.attachments)
  }, [installed, started, offer.request, offer.attachments, onContinue])

  return (
    <div className="mt-3 space-y-2 rounded-xl border border-line/70 bg-surface px-3 py-2.5 text-sm">
      {installed ? (
        <div className="flex flex-wrap items-center gap-2">
          <span>{started && offer.request ? t('setup.continuing') : t('setup.ready', { label: offer.label })}</span>
          {!started && offer.request ? (
            <button type="button" className="btn-secondary px-3 py-1 text-xs" onClick={() => onContinue(offer.request!, offer.attachments)}>
              {t('setup.continue')}
            </button>
          ) : null}
        </div>
      ) : started && !error ? (
        <div className="space-y-1.5">
          <div className="text-xs text-ink-muted">
            {t('setup.downloading', { done: gb((offer.size_bytes * (progress ?? 0)) / 100), total: gb(offer.size_bytes) })}
          </div>
          <div className="h-2 overflow-hidden rounded-full bg-raised" role="progressbar" aria-valuenow={progress ?? 0} aria-valuemin={0} aria-valuemax={100}>
            <div className="h-full bg-primary transition-[width] duration-500" style={{ width: `${progress ?? 0}%` }} />
          </div>
        </div>
      ) : (
        <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
          <span className="min-w-0 flex-1 text-xs text-ink-muted">
            {t('setup.details', { name: offer.name, size: gb(offer.size_bytes), node: t('setup.thisComputer') })}
          </span>
          <button
            type="button"
            className="btn-primary px-3 py-1.5 text-xs"
            onClick={async () => {
              setError(null)
              try {
                await api.installModel(offer.option)
                setStarted(true)
              } catch (err) {
                setError(shownError(err))
              }
            }}
          >
            {t('setup.install')}
          </button>
        </div>
      )}
      {error && !installed ? <ShownError {...error} className="text-xs text-danger" /> : null}
    </div>
  )
}
