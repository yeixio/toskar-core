import { useTranslation } from 'react-i18next'
import { useMutation, useQuery } from '@tanstack/react-query'
import { useRef, useState, type DragEvent } from 'react'
import { LoadingSpinner } from '@/components/ui/LoadingSpinner'
import { api, uploadModel } from '@/lib/api'
import { formatBytes } from '@/lib/format'
import { formatSize } from '@/i18n/format'
import { rovingKeyDown } from '@/lib/roving'
import type { BenchmarkJob, FoundModel, ModelImportResult } from '@/types/api'
import { ShownError, shownError } from '@/components/ui/ShownError'

type Way = 'file' | 'apps' | 'link'

// Each app's name, as people know it.
const APP_NAMES: Record<string, string> = { lmstudio: 'LM Studio', ollama: 'Ollama', llamacpp: 'llama.cpp', gpt4all: 'GPT4All' }

/**
 * Adds a model that isn't in the catalog (#467): a GGUF file, sent from
 * this browser or found by its location on this computer, one another
 * local AI app already downloaded, or a link. Models are added to this
 * computer.
 */
export function AddModelPanel({ onClose, onAdded }: { onClose: () => void; onAdded: (modelId: string) => void }) {
  const { t } = useTranslation('models')
  // Models other apps already have come first when there are any (#611):
  // they take no download and no extra space.
  const found = useQuery({ queryKey: ['models-found'], queryFn: () => api.findModelsInOtherApps(), retry: false })
  // While the scan runs, its own "Looking for models…" shows, so nothing
  // jumps away from a field someone is typing in.
  const appsFirst = found.isPending || (found.data?.models ?? []).some((m) => !m.model_id)
  const [chosen, setWay] = useState<Way | null>(null)
  const way: Way = chosen ?? (appsFirst ? 'apps' : 'file')
  const ways: Way[] = appsFirst ? ['apps', 'file', 'link'] : ['file', 'apps', 'link']
  const [done, setDone] = useState<string | null>(null)
  // The model just added: its name, and a vision projector to offer.
  const [lastAdded, setLastAdded] = useState<{ id: string; name: string; projector?: string } | null>(null)
  const added = (result: ModelImportResult | null, fallbackName?: string, projector?: string) => {
    if (!result) return
    // The name it was saved with: the one asked for, else its header's.
    const name = fallbackName || result.details?.name || result.model_id
    setLastAdded({ id: result.model_id, name, projector: projector || result.projector || undefined })
    setDone(result.status === 'copying' ? t('add.copying', { name }) : t('add.added', { name }))
    onAdded(result.model_id)
  }

  return (
    <section className="space-y-4 rounded-xl border border-line bg-surface p-4" aria-labelledby="add-model-title">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 id="add-model-title" className="section-title">
            {t('add.title')}
          </h2>
          <p className="mt-1 text-sm text-ink-muted">{t('add.description')}</p>
        </div>
        <button type="button" className="btn-secondary text-sm" onClick={onClose}>
          {t('browse.close')}
        </button>
      </div>

      <div role="tablist" aria-label={t('add.title')} className="segmented" onKeyDown={rovingKeyDown}>
        {ways.map((id) => (
          <button
            key={id}
            type="button"
            role="tab"
            aria-selected={way === id}
            tabIndex={way === id ? 0 : -1}
            className="segmented-item"
            onClick={() => {
              setWay(id)
              setDone(null)
              setLastAdded(null)
            }}
          >
            {t(`add.ways.${id}`)}
          </button>
        ))}
      </div>

      {way === 'file' ? <FromFile onAdded={added} /> : null}
      {way === 'apps' ? <FromApps onAdded={added} /> : null}
      {way === 'link' ? <FromLink onAdded={added} /> : null}

      {done ? (
        <p role="status" className="text-sm text-ink">
          {done}
        </p>
      ) : null}
      {lastAdded ? (
        <div key={lastAdded.id} className="space-y-3">
          <AddedDetails modelId={lastAdded.id} name={lastAdded.name} projector={lastAdded.projector} onSaved={() => onAdded(lastAdded.id)} />
          <QuickCheck modelId={lastAdded.id} />
        </div>
      ) : null}
    </section>
  )
}

// The tags a person can give a model they added; vision comes with a projector.
const TAGS = ['general', 'coding', 'reasoning', 'writing'] as const

/**
 * Names and tags a model just added, and gives it the vision projector
 * found with it (#467). Auto sends coding requests to a model tagged coding.
 */
function AddedDetails({ modelId, name, projector, onSaved }: { modelId: string; name: string; projector?: string; onSaved: () => void }) {
  const { t } = useTranslation('models')
  const [value, setValue] = useState(name)
  const [tags, setTags] = useState<string[]>(['general'])
  const [saved, setSaved] = useState(false)
  const save = useMutation({
    mutationFn: () => api.updateAddedModel(modelId, { display_name: value.trim(), tags }),
    onSuccess: () => {
      setSaved(true)
      onSaved()
    },
  })
  const pair = useMutation({
    mutationFn: () => api.setModelProjector(modelId, projector!),
    onSuccess: () => onSaved(),
  })
  return (
    <div className="space-y-3 rounded-lg border border-line/70 px-3 py-3 text-sm">
      <form
        className="space-y-2"
        onSubmit={(e) => {
          e.preventDefault()
          if (value.trim()) save.mutate()
        }}
      >
        <label className="block text-ink-muted">
          {t('add.details.name')}
          <input
            className="field mt-1 w-full"
            value={value}
            maxLength={120}
            onChange={(e) => {
              setValue(e.target.value)
              setSaved(false)
            }}
          />
        </label>
        <fieldset>
          <legend className="text-ink-muted">{t('add.details.tags')}</legend>
          <div className="mt-1 flex flex-wrap gap-x-4 gap-y-1">
            {TAGS.map((tag) => (
              <label key={tag} className="flex items-center gap-1.5 text-ink">
                <input
                  type="checkbox"
                  checked={tags.includes(tag)}
                  onChange={(e) => {
                    setTags((current) => (e.target.checked ? [...current, tag] : current.filter((x) => x !== tag)))
                    setSaved(false)
                  }}
                />
                {t(`add.details.tag.${tag}`)}
              </label>
            ))}
          </div>
          <p className="mt-1 text-xs text-ink-faint">{t('add.details.tagsHint')}</p>
        </fieldset>
        <div className="flex items-center gap-3">
          <button type="submit" className="btn-secondary btn-sm" disabled={!value.trim() || save.isPending}>
            {t('add.details.save')}
          </button>
          {saved ? <span className="text-xs text-ink-faint">{t('add.details.saved')}</span> : null}
        </div>
        {save.isError ? <ShownError {...shownError(save.error)} className="text-danger" /> : null}
      </form>
      {projector ? (
        <div className="flex flex-wrap items-center gap-3 border-t border-line/50 pt-3">
          {pair.isSuccess ? (
            <p className="text-ink">{t('add.details.visionOn')}</p>
          ) : (
            <>
              <p className="min-w-0 flex-1 text-ink-muted">
                {t('add.details.visionOffer')}
                <span className="block truncate font-mono text-xs text-ink-faint" title={projector}>
                  {projector.split(/[\\/]/).pop()}
                </span>
              </p>
              <button type="button" className="btn-secondary btn-sm" disabled={pair.isPending} onClick={() => pair.mutate()}>
                {t('add.details.visionAdd')}
              </button>
            </>
          )}
          {pair.isError ? <ShownError {...shownError(pair.error)} className="w-full text-danger" /> : null}
        </div>
      ) : null}
    </div>
  )
}

/** What a quick check found: the speed, or why it didn't answer. */
function checkResult(job: BenchmarkJob): { ok: boolean; speed?: number; error?: string } {
  const measured = job.samples.filter((s) => !s.warmup)
  const failed = job.samples.find((s) => s.error)
  if (job.status === 'failed' || (failed && measured.every((s) => s.error))) {
    return { ok: false, error: job.error || failed?.error }
  }
  const speeds = (measured.length ? measured : job.samples).map((s) => s.eval_tok_per_sec).filter((v) => v > 0)
  return { ok: true, speed: speeds.length ? speeds.reduce((a, b) => a + b, 0) / speeds.length : undefined }
}

/**
 * Loads a model just added and asks it one short question (#467): Works,
 * with its speed, or the error. Asked for, not automatic, since loading a
 * large model takes memory from what's running.
 */
function QuickCheck({ modelId }: { modelId: string }) {
  const { t } = useTranslation('models')
  // A copy is installed when it finishes; until then the check waits.
  const models = useQuery({
    queryKey: ['models'],
    queryFn: () => api.getModels(),
    retry: false,
    refetchInterval: (q) => ((q.state.data ?? []).find((x) => x.id === modelId)?.installed ? false : 2000),
  })
  const installed = Boolean((models.data ?? []).find((m) => m.id === modelId)?.installed)
  const [jobId, setJobId] = useState<string | null>(null)
  const start = useMutation({
    mutationFn: () => api.startBenchmark({ model_ids: [modelId], workload_ids: ['quick'], runs: 1 }),
    onSuccess: (job) => job && setJobId(job.id),
  })
  const job = useQuery({
    queryKey: ['benchmark', jobId],
    queryFn: () => api.getBenchmark(jobId!),
    enabled: Boolean(jobId),
    refetchInterval: (q) => (q.state.data && ['completed', 'failed', 'cancelled'].includes(q.state.data.status) ? false : 1000),
  })
  const finished = job.data && ['completed', 'failed', 'cancelled'].includes(job.data.status)
  const result = finished && job.data ? checkResult(job.data) : null
  return (
    <div className="flex flex-wrap items-center gap-3 rounded-lg border border-line/70 px-3 py-2 text-sm">
      {result ? (
        result.ok ? (
          <p className="text-ink">
            <span className="font-medium text-success">{t('add.check.works')}</span>
            {result.speed ? ` · ${t('add.check.speed', { speed: Math.round(result.speed) })}` : ''}
          </p>
        ) : (
          <p className="text-danger">{t('add.check.failed', { error: result.error || t('add.failed') })}</p>
        )
      ) : jobId || start.isPending ? (
        <LoadingSpinner label={t('add.check.running')} />
      ) : (
        <>
          <span className="text-ink-muted">{installed ? t('add.check.prompt') : t('add.check.waiting')}</span>
          <button type="button" className="btn-secondary btn-sm" disabled={!installed} onClick={() => start.mutate()}>
            {t('add.check.button')}
          </button>
        </>
      )}
      {start.isError ? <ShownError {...shownError(start.error)} className="text-danger" /> : null}
    </div>
  )
}

function FromFile({ onAdded }: { onAdded: (r: ModelImportResult | null, name?: string, projector?: string) => void }) {
  const { t } = useTranslation('models')
  const input = useRef<HTMLInputElement>(null)
  const [dragging, setDragging] = useState(false)
  const [progress, setProgress] = useState<{ sent: number; total: number } | null>(null)
  const [path, setPath] = useState('')
  const [inPlace, setInPlace] = useState(false)
  const upload = useMutation({
    mutationFn: (file: File) => uploadModel(file, (sent, total) => setProgress({ sent, total })),
    onSuccess: (r) => onAdded(r),
    onSettled: () => setProgress(null),
  })
  const byPath = useMutation({
    mutationFn: () => api.importModelPath({ path: path.trim(), in_place: inPlace }),
    onSuccess: (r) => onAdded(r),
  })
  const send = (file?: File) => {
    if (!file) return
    upload.reset()
    upload.mutate(file)
  }
  const onDrop = (e: DragEvent) => {
    e.preventDefault()
    setDragging(false)
    send(e.dataTransfer.files[0])
  }

  return (
    <div className="space-y-4">
      <div
        onDragOver={(e) => {
          e.preventDefault()
          setDragging(true)
        }}
        onDragLeave={() => setDragging(false)}
        onDrop={onDrop}
        className={[
          'flex flex-col items-center gap-2 rounded-xl border-2 border-dashed px-4 py-6 text-center text-sm',
          dragging ? 'border-primary bg-primary-soft' : 'border-line',
        ].join(' ')}
      >
        {progress ? (
          <>
            <p className="text-ink">{t('add.file.sending', { sent: formatBytes(progress.sent), total: formatBytes(progress.total) })}</p>
            <progress className="w-full max-w-sm" value={progress.sent} max={progress.total || 1} />
          </>
        ) : (
          <>
            <p className="text-ink-muted">{t('add.file.drop')}</p>
            <button type="button" className="btn-primary btn-sm" disabled={upload.isPending} onClick={() => input.current?.click()}>
              {t('add.file.choose')}
            </button>
            <input
              ref={input}
              type="file"
              accept=".gguf"
              className="sr-only"
              aria-label={t('add.file.choose')}
              onChange={(e) => {
                send(e.target.files?.[0])
                e.target.value = ''
              }}
            />
          </>
        )}
      </div>
      {upload.isError ? <ShownError {...shownError(upload.error)} className="text-sm text-danger" /> : null}

      <form
        className="space-y-2"
        onSubmit={(e) => {
          e.preventDefault()
          if (path.trim()) byPath.mutate()
        }}
      >
        <label className="block text-sm text-ink-muted">
          {t('add.file.pathLabel')}
          <input
            className="field mt-1 w-full font-mono text-xs"
            value={path}
            placeholder={t('add.file.pathPlaceholder')}
            onChange={(e) => setPath(e.target.value)}
            spellCheck={false}
          />
        </label>
        <fieldset className="space-y-1 text-sm">
          <legend className="sr-only">{t('add.file.how')}</legend>
          <label className="flex items-start gap-2">
            <input type="radio" name="import-how" className="mt-1" checked={!inPlace} onChange={() => setInPlace(false)} />
            <span>
              <span className="text-ink">{t('add.file.copy')}</span>
              <span className="block text-xs text-ink-faint">{t('add.file.copyHint')}</span>
            </span>
          </label>
          <label className="flex items-start gap-2">
            <input type="radio" name="import-how" className="mt-1" checked={inPlace} onChange={() => setInPlace(true)} />
            <span>
              <span className="text-ink">{t('add.file.inPlace')}</span>
              <span className="block text-xs text-ink-faint">{t('add.file.inPlaceHint')}</span>
            </span>
          </label>
        </fieldset>
        <button type="submit" className="btn-secondary btn-sm" disabled={!path.trim() || byPath.isPending}>
          {byPath.isPending ? t('add.adding') : t('add.add')}
        </button>
        {byPath.isError ? <ShownError {...shownError(byPath.error)} className="text-sm text-danger" /> : null}
      </form>
    </div>
  )
}

function FromApps({ onAdded }: { onAdded: (r: ModelImportResult | null, name?: string, projector?: string) => void }) {
  const { t } = useTranslation('models')
  const found = useQuery({ queryKey: ['models-found'], queryFn: () => api.findModelsInOtherApps(), retry: false })
  const add = useMutation({
    mutationFn: (m: FoundModel) => api.importModelPath({ path: m.path, in_place: true, display_name: m.name }),
    onSuccess: (r, m) => {
      onAdded(r, m.name, m.projector)
      void found.refetch()
    },
  })
  const list = found.data?.models ?? []
  if (found.isLoading) return <LoadingSpinner label={t('add.apps.looking')} />
  return (
    <div className="space-y-3">
      <p className="text-sm text-ink-muted">{t('add.apps.description')}</p>
      {list.length === 0 ? (
        <p className="rounded-xl border border-line px-4 py-3 text-sm text-ink-muted">{t('add.apps.none')}</p>
      ) : (
        <ul className="divide-y divide-line rounded-xl border border-line">
          {list.map((m) => (
            <li key={m.path} className="flex min-w-0 flex-wrap items-center justify-between gap-3 px-4 py-3">
              <div className="min-w-0">
                <p className="font-medium text-ink">{m.name}</p>
                <p className="mt-0.5 truncate text-xs text-ink-muted" title={m.path}>
                  {[APP_NAMES[m.app] ?? m.app, m.parameters, m.quantization, formatBytes(m.size_bytes)].filter(Boolean).join(' · ')}
                </p>
              </div>
              {m.model_id ? (
                <span className="text-xs text-ink-faint">{t('add.apps.already')}</span>
              ) : (
                <button
                  type="button"
                  className="btn-primary btn-sm shrink-0"
                  disabled={add.isPending}
                  onClick={() => add.mutate(m)}
                >
                  {add.isPending && add.variables?.path === m.path ? t('add.adding') : t('add.add')}
                </button>
              )}
            </li>
          ))}
        </ul>
      )}
      {found.isError ? <ShownError {...shownError(found.error)} className="text-sm text-danger" /> : null}
      {add.isError ? <ShownError {...shownError(add.error)} className="text-sm text-danger" /> : null}
    </div>
  )
}

/**
 * A model from a link (#611): first what it would download, then the
 * download. It says the size, the space left, and that it's a free
 * download saved on this computer, and offers a copy another app already
 * has, used where it is, before downloading a second one.
 */
function FromLink({ onAdded }: { onAdded: (r: ModelImportResult | null, name?: string, projector?: string) => void }) {
  const { t } = useTranslation('models')
  const [url, setUrl] = useState('')
  const [name, setName] = useState('')
  const preview = useMutation({ mutationFn: () => api.previewModelURL(url.trim()) })
  const install = useMutation({
    mutationFn: () => api.installModelFromURL({ source_url: url.trim(), display_name: name.trim() || undefined }),
    onSuccess: (r) => r && onAdded({ model_id: r.model_id, status: 'copying', details: {} }, name.trim() || undefined),
  })
  const useCopy = useMutation({
    mutationFn: (m: FoundModel) => api.importModelPath({ path: m.path, in_place: true, display_name: name.trim() || m.name }),
    onSuccess: (r, m) => onAdded(r, name.trim() || m.name, m.projector),
  })
  const p = preview.data
  // In the units the repository and the file browser show (decimal), so
  // the size matches what the person sees on the download page.
  const size = (n: number) => formatSize(n, { base: 1000 })
  const total = (p?.size_bytes ?? 0) + (p?.projector_bytes ?? 0)
  const tooBig = Boolean(p?.free_bytes && total > p.free_bytes)
  const copy = p?.elsewhere?.[0]
  const host = (() => {
    try {
      return new URL(url.trim()).host
    } catch {
      return ''
    }
  })()
  return (
    <form
      className="space-y-3"
      onSubmit={(e) => {
        e.preventDefault()
        if (url.trim()) preview.mutate()
      }}
    >
      <p className="text-sm text-ink-muted">{t('add.link.description')}</p>
      <label className="block text-sm text-ink-muted">
        {t('add.link.url')}
        <input
          className="field mt-1 w-full font-mono text-xs"
          type="url"
          value={url}
          placeholder="https://huggingface.co/…/resolve/main/model-Q4_K_M.gguf"
          onChange={(e) => {
            setUrl(e.target.value)
            preview.reset()
          }}
          spellCheck={false}
        />
      </label>
      <label className="block text-sm text-ink-muted">
        {t('add.link.name')}
        <input className="field mt-1 w-full" value={name} onChange={(e) => setName(e.target.value)} />
      </label>
      {!p ? (
        <button type="submit" className="btn-secondary btn-sm" disabled={!url.trim() || preview.isPending}>
          {preview.isPending ? t('add.link.checking') : t('add.link.check')}
        </button>
      ) : (
        <div className="space-y-3 rounded-xl border border-line p-3">
          {p.installed ? <p className="text-sm text-ink">{t('add.link.installed', { id: p.installed_as })}</p> : null}
          {copy ? (
            <div className="space-y-2 rounded-lg bg-primary-soft/40 p-3">
              <p className="text-sm text-ink">
                {t('add.link.haveIt', { app: APP_NAMES[copy.app] ?? copy.app, size: size(copy.size_bytes) })}
              </p>
              <p className="truncate text-xs text-ink-muted" title={copy.path}>
                {copy.path}
              </p>
              <button type="button" className="btn-primary btn-sm" disabled={useCopy.isPending} onClick={() => useCopy.mutate(copy)}>
                {useCopy.isPending ? t('add.adding') : t('add.link.useCopy')}
              </button>
            </div>
          ) : null}
          <p className="text-sm text-ink-muted">
            {total > 0
              ? t(p.projector_bytes ? 'add.link.downloadSizeVision' : 'add.link.downloadSize', {
                  size: size(total),
                  host,
                })
              : t('add.link.downloadUnknown', { host })}
            {p.free_bytes ? ` ${t('add.link.free', { free: size(p.free_bytes) })}` : ''}
          </p>
          {tooBig ? (
            <p className="text-sm text-danger" role="alert">
              {t('add.link.noRoom', { size: size(total), free: size(p.free_bytes ?? 0) })}
            </p>
          ) : null}
          <button
            type="button"
            className={copy || p.installed ? 'btn-secondary btn-sm' : 'btn-primary btn-sm'}
            disabled={tooBig || install.isPending}
            onClick={() => install.mutate()}
          >
            {install.isPending
              ? t('add.adding')
              : copy || p.installed
                ? t('add.link.downloadAnyway')
                : total > 0
                  ? t('add.link.download', { size: size(total) })
                  : t('add.link.downloadPlain')}
          </button>
        </div>
      )}
      {preview.isError ? <ShownError {...shownError(preview.error)} className="text-sm text-danger" /> : null}
      {install.isError ? <ShownError {...shownError(install.error)} className="text-sm text-danger" /> : null}
      {useCopy.isError ? <ShownError {...shownError(useCopy.error)} className="text-sm text-danger" /> : null}
    </form>
  )
}
