import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { formatSize } from '@/i18n/format'
import { api, type MediaKind } from '@/lib/api'
import type { ImageSetup } from '@/types/api'

function gb(bytes: number): string {
  return formatSize(bytes, { base: 1000 })
}

// Wording that differs by kind is tools:<kind>.<key>; the rest is shared
// under tools:images.
const OWN = new Set(['title', 'unsupported', 'ready', 'notSetUp', 'makesAndEdits', 'makes', 'setUp', 'slow'])

// headroom is memory beyond a model's files that running it needs, as the
// daemon counts it.
const headroom = 2 * 1024 ** 3

// What a tight model is short of, said with what it needs and what this
// computer has (#550).
const shortKeys = { memory: 'images.tightMemory', graphics: 'images.graphicsShort', shared: 'images.sharedShort' } as const

/** Image generation (Gungnir §17). */
export function ImageSetupCard() {
  return <MediaSetupCard kind="images" />
}

/** Video generation (Gungnir §27). */
export function VideoSetupCard() {
  return <MediaSetupCard kind="video" />
}

// Hugging Face read tokens start with hf_; the hint isn't translated.
const TOKEN_HINT = 'hf_…'

/**
 * Set up a kind of generated media with a recommended model, watch the
 * download, and remove models to free space.
 */
function MediaSetupCard({ kind }: { kind: MediaKind }) {
  const { t: tt } = useTranslation('tools')
  const t = (key: string, opts?: Record<string, unknown>) => tt(`${OWN.has(key.split('.')[1]) ? kind : 'images'}.${key.split('.')[1]}`, opts)
  const queryClient = useQueryClient()
  const [choice, setChoice] = useState<string | null>(null)
  const setupQuery = useQuery({
    queryKey: [kind, 'setup'],
    queryFn: () => api.getMediaSetup(kind),
    retry: false,
    // Follow a setup while it runs.
    refetchInterval: (query) => (query.state.data?.job?.running ? 1000 : false),
  })
  const update = (status: ImageSetup | null) => {
    if (status) queryClient.setQueryData([kind, 'setup'], status)
    else void queryClient.invalidateQueries({ queryKey: [kind, 'setup'] })
    // Tools become ready, or stop being ready.
    void queryClient.invalidateQueries({ queryKey: ['tools'] })
  }
  const start = useMutation({
    mutationFn: ({ id, accept, tight }: { id: string; accept?: boolean; tight?: boolean }) =>
      api.startMediaSetup(kind, id, undefined, { acceptLicense: accept, acceptTight: tight }),
    onSuccess: update,
  })
  // A model behind a license: the person's token, and their word (#551).
  const [licenseOK, setLicenseOK] = useState(false)
  // A model that would be slow or near the limit here: the person agrees (#550).
  const [tightOK, setTightOK] = useState(false)
  const [token, setToken] = useState('')
  // A model that's tight or too large here can be set up on a paired
  // computer it works well on (#550).
  const [startedOn, setStartedOn] = useState<string | null>(null)
  const startThere = useMutation({
    mutationFn: ({ id, node }: { id: string; node: { node_id: string; node_name: string } }) =>
      api.startMediaSetup(kind, id, node.node_id).then(() => node.node_name),
    onSuccess: (name) => setStartedOn(name),
  })
  const saveToken = useMutation({
    mutationFn: (value: string) => api.updateSettings({ huggingface_token: value }),
    onSuccess: () => {
      setToken('')
      void queryClient.invalidateQueries({ queryKey: [kind, 'setup'] })
    },
  })
  const cancel = useMutation({ mutationFn: () => api.cancelMediaSetup(kind), onSuccess: update })
  const remove = useMutation({ mutationFn: (id: string) => api.removeMediaModel(kind, id), onSuccess: update })
  const switchBuild = useMutation({ mutationFn: (build: 'gpu' | 'cpu') => api.useMediaBuild(kind, build), onSuccess: update })

  // When a setup ends, the image tools become ready (or stay not ready).
  const wasRunning = useRef(false)
  const runningNow = !!setupQuery.data?.job?.running
  useEffect(() => {
    if (wasRunning.current && !runningNow) void queryClient.invalidateQueries({ queryKey: ['tools'] })
    wasRunning.current = runningNow
  }, [runningNow, queryClient])

  const status = setupQuery.data
  if (!status) return null
  const job = status.job
  const running = !!job?.running
  const active = status.models.find((m) => m.id === status.active)
  // A model this computer can't hold isn't chosen or set up.
  const fits = status.models.filter((m) => !m.too_little_memory)
  const selected = choice ?? (fits.find((m) => m.recommended) ?? fits[0])?.id
  const selectedModel = status.models.find((m) => m.id === selected)
  const failed = start.error ?? remove.error ?? switchBuild.error
  const error = failed instanceof Error ? failed.message : job && !job.running ? job.error : undefined
  const percent = job && job.total_bytes > 0 ? Math.round((job.done_bytes / job.total_bytes) * 100) : 0

  return (
    <section className="card space-y-3" aria-labelledby={`${kind}-setup-title`}>
      <div>
        <h2 id={`${kind}-setup-title`} className="section-title">
          {t('images.title')}
        </h2>
        <p className="mt-1 max-w-2xl text-sm text-ink-muted">
          {!status.supported
            ? t('images.unsupported', { reason: status.unsupported })
            : status.ready && active
              ? t('images.ready', { model: active.name })
              : t('images.notSetUp')}
        </p>
      </div>

      {status.supported && status.accelerated === false && !status.gpu_build ? <p className="text-xs text-ink-muted">{t('images.slow')}</p> : null}

      {/* Which build makes them, and a way to switch: the GPU build where one runs here, or the CPU build (#154). */}
      {status.supported && status.program && status.build && !running ? (
        <div className="flex flex-wrap items-center gap-3 text-xs text-ink-muted">
          <span>{status.build === 'cpu' ? t('images.onCPU') : t('images.onGPU', { build: status.build === 'metal' ? 'Metal' : 'Vulkan' })}</span>
          {status.build === 'cpu' && status.gpu_build ? (
            <button type="button" className="btn-secondary px-3 py-1 text-xs" disabled={switchBuild.isPending} onClick={() => switchBuild.mutate('gpu')}>
              {t('images.useGPU')}
            </button>
          ) : null}
          {status.build === 'vulkan' ? (
            <button type="button" className="text-xs text-ink-faint underline-offset-2 hover:text-ink hover:underline" disabled={switchBuild.isPending} onClick={() => switchBuild.mutate('cpu')}>
              {t('images.useCPU')}
            </button>
          ) : null}
        </div>
      ) : null}

      {status.supported && running && job ? (
        <div className="space-y-1.5">
          <div className="flex items-center justify-between text-xs text-ink-muted">
            <span>{job.stage === 'program' ? t('images.installingProgram') : t('images.downloadingModel')}</span>
            <span className="tabular-nums">{t('images.progress', { done: gb(job.done_bytes), total: gb(job.total_bytes) })}</span>
          </div>
          <div className="h-2 overflow-hidden rounded-full bg-raised" role="progressbar" aria-valuenow={percent} aria-valuemin={0} aria-valuemax={100}>
            <div className="h-full bg-primary transition-[width] duration-500" style={{ width: `${percent}%` }} />
          </div>
          <button type="button" className="btn-secondary px-3 py-1.5 text-xs" disabled={cancel.isPending} onClick={() => cancel.mutate()}>
            {t('images.stop')}
          </button>
        </div>
      ) : null}

      {status.supported && !running ? (
        <ul className="space-y-2">
          {status.models.map((model) => (
            <li key={model.id} className="flex flex-wrap items-start gap-3 rounded-lg border border-line/70 p-3">
              {!model.installed ? (
                <input
                  type="radio"
                  name={`${kind}-model`}
                  className="mt-1"
                  checked={selected === model.id}
                  disabled={model.too_little_memory}
                  onChange={() => setChoice(model.id)}
                  aria-label={model.name}
                />
              ) : null}
              <div className="min-w-0 flex-1">
                <p className="text-sm font-medium text-ink">
                  {model.name}
                  {model.recommended ? <span className="ms-2 text-xs font-normal text-success">{t('images.recommended')}</span> : null}
                  {model.untested ? <span className="ms-2 text-xs font-normal text-warning">{t('images.untested')}</span> : null}
                  {model.needs_license ? <span className="ms-2 text-xs font-normal text-warning">{t('images.needsLicense')}</span> : null}
                  {model.id === status.active ? <span className="ms-2 text-xs font-normal text-ink-faint">{t('images.inUse')}</span> : null}
                </p>
                <p className="mt-0.5 text-xs text-ink-muted">{model.description}</p>
                <p className="mt-0.5 text-xs text-ink-faint">
                  {gb(model.size_bytes)} · {model.license}
                  {model.edits ? t('images.makesAndEdits') : t('images.makes')}
                </p>
                {model.too_little_memory ? (
                  <p className="mt-1 text-xs text-danger">
                    {t('images.tooLittleMemory', { need: gb(model.size_bytes + headroom), have: gb(status.memory_bytes ?? 0) })}
                  </p>
                ) : model.verdict === 'tight' && model.short ? (
                  <p className="mt-1 text-xs text-warning">
                    {t(shortKeys[model.short], { need: gb(model.need_bytes ?? 0), have: gb(model.have_bytes ?? 0) })}
                  </p>
                ) : null}
                {model.verdict && model.verdict !== 'works'
                  ? (status.elsewhere ?? [])
                      .filter((place) => place.model_id === model.id)
                      .slice(0, 2)
                      .map((place) => (
                        <p key={place.node_id} className="mt-1 flex flex-wrap items-center gap-2 text-xs text-ink-muted">
                          <span>
                            {place.installed
                              ? t('images.elsewhereInstalled', { node: place.node_name })
                              : t('images.elsewhereWorks', { node: place.node_name })}
                          </span>
                          {!place.installed ? (
                            <button
                              type="button"
                              className="btn-secondary px-2 py-0.5 text-xs"
                              disabled={startThere.isPending}
                              onClick={() => startThere.mutate({ id: model.id, node: place })}
                            >
                              {t('images.elsewhereSetUp')}
                            </button>
                          ) : null}
                        </p>
                      ))
                  : null}
              </div>
              {model.installed ? (
                <span className="flex gap-2">
                  {model.id !== status.active ? (
                    <button type="button" className="btn-secondary px-3 py-1.5 text-xs" onClick={() => start.mutate({ id: model.id })}>
                      {t('images.use')}
                    </button>
                  ) : null}
                  <button
                    type="button"
                    className="btn-secondary px-3 py-1.5 text-xs"
                    disabled={remove.isPending}
                    onClick={() => remove.mutate(model.id)}
                  >
                    {t('images.remove')}
                  </button>
                </span>
              ) : null}
            </li>
          ))}
        </ul>
      ) : null}

      {status.supported && !running && selectedModel?.needs_license && !selectedModel.installed && !selectedModel.too_little_memory ? (
        <div className="space-y-2 rounded-lg border border-warning/40 bg-warning/5 p-3 text-xs">
          <p className="text-ink">
            {selectedModel.license_allows}{' '}
            <a className="underline underline-offset-2" href={selectedModel.license_url} target="_blank" rel="noreferrer">
              {t('images.licenseLink')}
            </a>
          </p>
          {status.huggingface_token_set ? (
            <p className="text-ink-muted">{t('images.tokenSet')}</p>
          ) : (
            <div className="space-y-1">
              <label className="block text-ink-muted">
                {t('images.tokenLabel')}
                <span className="mt-1 flex gap-2">
                  <input
                    type="password"
                    autoComplete="off"
                    className="field min-w-0 flex-1 py-1 text-xs"
                    value={token}
                    placeholder={TOKEN_HINT}
                    onChange={(e) => setToken(e.target.value)}
                  />
                  <button type="button" className="btn-secondary px-3 py-1 text-xs" disabled={!token.trim() || saveToken.isPending} onClick={() => saveToken.mutate(token.trim())}>
                    {t('images.tokenSave')}
                  </button>
                </span>
              </label>
              <p className="text-ink-faint">{t('images.tokenHelp')}</p>
              {saveToken.error instanceof Error ? <p className="text-danger">{saveToken.error.message}</p> : null}
            </div>
          )}
          <label className="flex items-start gap-2 text-ink">
            <input type="checkbox" className="mt-0.5" checked={licenseOK || !!selectedModel.license_accepted} onChange={(e) => setLicenseOK(e.target.checked)} />
            <span>{t('images.licenseConfirm')}</span>
          </label>
        </div>
      ) : null}

      {status.supported && !running && selectedModel?.verdict === 'tight' && !selectedModel.installed ? (
        <label className="flex items-start gap-2 text-xs text-ink">
          <input type="checkbox" className="mt-0.5" checked={tightOK} onChange={(e) => setTightOK(e.target.checked)} />
          <span>{t('images.tightConfirm')}</span>
        </label>
      ) : null}

      {status.supported && !running && selected && !selectedModel?.installed && !selectedModel?.too_little_memory ? (
        <button
          type="button"
          className="btn-primary px-4 py-2 text-sm"
          disabled={
            start.isPending ||
            (selectedModel?.verdict === 'tight' && !tightOK) ||
            (!!selectedModel?.needs_license && (!status.huggingface_token_set || !(licenseOK || selectedModel.license_accepted)))
          }
          onClick={() =>
            start.mutate({ id: selected, accept: selectedModel?.needs_license ? true : undefined, tight: selectedModel?.verdict === 'tight' ? true : undefined })
          }
        >
          {status.ready ? t('images.download') : t('images.setUp')}
        </button>
      ) : null}

      {startedOn ? <p className="text-xs text-ink-muted">{t('images.elsewhereStarted', { node: startedOn })}</p> : null}
      {startThere.error instanceof Error ? <p className="text-xs text-danger">{startThere.error.message}</p> : null}
      {error ? <p className="text-xs text-danger">{error}</p> : null}
    </section>
  )
}
