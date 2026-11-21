import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState, type ChangeEvent, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '@/lib/api'
import { useRole } from '@/lib/role'
import type { ComfyWorkflowInputs } from '@/types/api'
import { ShownError, shownError, type Shown } from '@/components/ui/ShownError'
import { LoadError } from '@/components/ui/LoadError'

const KEY = ['comfyui']
const FLOWS = ['comfyui', 'workflows']
const INPUTS = ['prompt', 'negative', 'seed', 'image', 'width', 'height'] as const

/** The inputs a workflow takes, in words: "prompt (node 6), seed (node 3)". */
function inputList(inputs: ComfyWorkflowInputs, t: (key: string, o?: Record<string, unknown>) => string): string {
  const found = INPUTS.filter((k) => inputs[k]).map((k) => t('comfyui.input', { name: t(`comfyui.inputs.${k}`), node: inputs[k]!.node }))
  return found.length ? found.join(', ') : t('comfyui.noInputs')
}

/**
 * A ComfyUI the Admin runs, here or on another computer (#958): its saved
 * workflows run as the comfyui.run tool beside the built-in engine. Only
 * Admins see it, as custom nodes run code on that computer.
 */
export function ComfyUICard() {
  const { atLeast } = useRole()
  if (!atLeast('admin')) return null
  return <ComfyUISettings />
}

function ComfyUISettings() {
  const { t } = useTranslation('tools')
  const queryClient = useQueryClient()
  const status = useQuery({ queryKey: KEY, queryFn: () => api.getComfyUI(), retry: false })
  const [edited, setEdited] = useState<string | null>(null)
  const [token, setToken] = useState('')
  const [error, setError] = useState<Shown | null>(null)
  const url = edited ?? status.data?.base_url ?? ''

  const save = useMutation({
    mutationFn: (body: { base_url: string; token?: string; clear_token?: boolean }) => api.setComfyUI(body),
    onSuccess: (next) => {
      setToken('')
      setEdited(null)
      setError(null)
      queryClient.setQueryData(KEY, next)
      void queryClient.invalidateQueries({ queryKey: ['tools'] })
    },
    onError: (err) => setError(shownError(err)),
  })
  const submit = (e: FormEvent) => {
    e.preventDefault()
    save.mutate({ base_url: url.trim(), ...(token.trim() ? { token: token.trim() } : {}) })
  }
  const data = status.data
  const info = data?.info

  return (
    <section className="card space-y-4" aria-labelledby="comfyui-title">
      <div>
        <h2 id="comfyui-title" className="section-title">
          {t('comfyui.title')}
        </h2>
        <p className="mt-1 text-sm text-ink-muted">{t('comfyui.description')}</p>
      </div>
      {status.isError ? <LoadError compact error={status.error} onRetry={() => void status.refetch()} retrying={status.isFetching} /> : null}
      <form className="space-y-3" onSubmit={submit}>
        <label className="block text-sm">
          <span className="text-ink-muted">{t('comfyui.url')}</span>
          <input className="field mt-1 w-full" type="url" placeholder="http://127.0.0.1:8188" value={url} onChange={(e) => setEdited(e.target.value)} />
        </label>
        <label className="block text-sm">
          <span className="text-ink-muted">{t('comfyui.token')}</span>
          <input
            className="field mt-1 w-full"
            type="password"
            autoComplete="off"
            placeholder={data?.has_token ? t('comfyui.tokenStored') : t('comfyui.tokenOptional')}
            value={token}
            onChange={(e) => setToken(e.target.value)}
          />
        </label>
        <div className="flex flex-wrap gap-2">
          <button type="submit" className="btn-primary px-3 py-1.5 text-xs" disabled={save.isPending || !status.isSuccess}>
            {save.isPending ? t('comfyui.checking') : t('comfyui.save')}
          </button>
          {data?.has_token ? (
            <button type="button" className="btn-secondary px-3 py-1.5 text-xs" disabled={save.isPending} onClick={() => save.mutate({ base_url: url.trim(), clear_token: true })}>
              {t('comfyui.forgetToken')}
            </button>
          ) : null}
        </div>
      </form>
      {error ? (
        <ShownError {...error} role="alert" />
      ) : data?.base_url ? (
        data.error ? (
          <ShownError error={data.error} role="alert" />
        ) : info ? (
          <div className="space-y-1 text-sm text-ink-muted" role="status">
            <p>{t('comfyui.connected', { version: info.version || '?', nodes: info.node_types })}</p>
            {info.devices.map((d) => (
              <p key={d.name}>{t('comfyui.device', { name: d.name, gb: Math.round(d.vram_total / 1e9) })}</p>
            ))}
            <p>{t('comfyui.models', { checkpoints: (info.models.checkpoints?.length ?? 0) + (info.models.diffusion?.length ?? 0), loras: info.models.loras?.length ?? 0 })}</p>
          </div>
        ) : null
      ) : null}
      {data?.base_url ? <Workflows /> : null}
      <p className="text-xs text-ink-faint">{t('comfyui.safety')}</p>
    </section>
  )
}

function Workflows() {
  const { t } = useTranslation('tools')
  const queryClient = useQueryClient()
  const flows = useQuery({ queryKey: FLOWS, queryFn: () => api.listComfyWorkflows() })
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [graph, setGraph] = useState<unknown>(null)
  const [found, setFound] = useState<ComfyWorkflowInputs | null>(null)
  const [error, setError] = useState<Shown | null>(null)

  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: FLOWS })
    void queryClient.invalidateQueries({ queryKey: ['tools'] })
  }
  const remove = useMutation({ mutationFn: (n: string) => api.deleteComfyWorkflow(n), onSuccess: refresh, onError: (err) => setError(shownError(err)) })
  const save = useMutation({
    mutationFn: () => api.saveComfyWorkflow({ name: name.trim(), description: description.trim() || undefined, graph, inputs: found ?? undefined }),
    onSuccess: () => {
      setName('')
      setDescription('')
      setGraph(null)
      setFound(null)
      setError(null)
      refresh()
    },
    onError: (err) => setError(shownError(err)),
  })

  const pick = async (e: ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    e.target.value = ''
    if (!file) return
    setFound(null)
    setError(null)
    try {
      const parsed: unknown = JSON.parse(await file.text())
      setGraph(parsed)
      if (!name) setName(file.name.replace(/\.json$/i, ''))
      setFound(await api.detectComfyWorkflow(parsed))
    } catch (err) {
      setGraph(null)
      setError(shownError(err instanceof SyntaxError ? new Error(t('comfyui.notJSON')) : err))
    }
  }
  const submit = (e: FormEvent) => {
    e.preventDefault()
    save.mutate()
  }
  const list = flows.data ?? []

  return (
    <div className="space-y-3 border-t border-line pt-3">
      <h3 className="text-sm font-semibold">{t('comfyui.workflows')}</h3>
      {flows.isError ? <LoadError compact error={flows.error} onRetry={() => void flows.refetch()} retrying={flows.isFetching} /> : null}
      {flows.isSuccess && list.length === 0 ? <p className="text-sm text-ink-muted">{t('comfyui.noWorkflows')}</p> : null}
      <ul className="space-y-2">
        {list.map((w) => (
          <li key={w.name} className="flex items-start justify-between gap-3 text-sm">
            <div className="min-w-0">
              <p className="font-medium">{w.name}</p>
              {w.description ? <p className="text-ink-muted">{w.description}</p> : null}
              <p className="text-xs text-ink-faint">{inputList(w.inputs, t)}</p>
            </div>
            <button
              type="button"
              className="btn-secondary shrink-0 px-2 py-1 text-xs"
              aria-label={t('comfyui.removeNamed', { name: w.name })}
              disabled={remove.isPending}
              onClick={() => remove.mutate(w.name)}
            >
              {t('comfyui.remove')}
            </button>
          </li>
        ))}
      </ul>
      <form className="space-y-3" onSubmit={submit}>
        <div className="text-sm">
          <label className="block">
            <span className="text-ink-muted">{t('comfyui.file')}</span>
            <input className="mt-1 block w-full text-sm" type="file" accept=".json,application/json" aria-describedby="comfyui-file-hint" onChange={(e) => void pick(e)} />
          </label>
          <p id="comfyui-file-hint" className="mt-1 text-xs text-ink-faint">
            {t('comfyui.fileHint')}
          </p>
        </div>
        {found ? <p className="text-sm text-ink-muted">{t('comfyui.found', { inputs: inputList(found, t) })}</p> : null}
        <label className="block text-sm">
          <span className="text-ink-muted">{t('comfyui.name')}</span>
          <input className="field mt-1 w-full" value={name} onChange={(e) => setName(e.target.value)} placeholder="upscale" />
        </label>
        <label className="block text-sm">
          <span className="text-ink-muted">{t('comfyui.what')}</span>
          <input className="field mt-1 w-full" value={description} onChange={(e) => setDescription(e.target.value)} placeholder={t('comfyui.whatPlaceholder')} />
        </label>
        <button type="submit" className="btn-primary px-3 py-1.5 text-xs" disabled={!graph || !name.trim() || save.isPending}>
          {t('comfyui.add')}
        </button>
      </form>
      {error ? <ShownError {...error} role="alert" /> : null}
    </div>
  )
}
