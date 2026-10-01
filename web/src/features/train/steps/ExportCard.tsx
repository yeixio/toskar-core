import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { formatBytes } from '@/lib/format'
import type { SpecializedAIView } from '@/types/api'
import { errorText, exportRevision } from '../display'

// ExportCard merges a revision into one GGUF file that other GGUF tools can
// load without Yggdrasil.
export function ExportCard({ view }: { view: SpecializedAIView }) {
  const revision = exportRevision(view)
  const queryClient = useQueryClient()
  const key = ['training', view.id, 'export', revision]
  const status = useQuery({
    queryKey: key,
    queryFn: () => api.exportStatus(view.id, revision),
    enabled: revision > 0,
    refetchInterval: (q) => (q.state.data?.state === 'exporting' ? 2000 : false),
  })
  const refresh = () => void queryClient.invalidateQueries({ queryKey: key })
  const start = useMutation({ mutationFn: () => api.startExport(view.id, revision), onSuccess: refresh })
  const remove = useMutation({ mutationFn: () => api.deleteExport(view.id, revision), onSuccess: refresh })
  if (revision === 0) return null

  const st = status.data
  const error = start.error ?? remove.error ?? status.error
  return (
    <div className="card space-y-3">
      <h3 className="section-title">Export as a GGUF file</h3>
      <p className="text-sm text-ink-muted">
        Merge revision {revision} into its base model as one file that llama.cpp, LM Studio, Ollama, and other GGUF
        tools can load. The file is about the size of the base model. Its instructions and connected knowledge stay in
        Yggdrasil.
      </p>
      {st?.state === 'exporting' && (
        <p className="text-sm text-ink" role="status">
          Exporting… {st.size_bytes ? `up to ${formatBytes(st.size_bytes)}` : ''}
        </p>
      )}
      {st?.state === 'failed' && <p className="text-sm text-danger">The export failed: {st.error}</p>}
      {st?.state === 'ready' && (
        <div className="flex flex-wrap items-center gap-2 text-sm">
          <a className="btn-primary px-3 py-1 text-xs" href={api.exportFileUrl(view.id, revision)} download={st.filename}>
            Download {st.filename}
          </a>
          <span className="text-xs text-ink-muted">{formatBytes(st.size_bytes)}</span>
          <button type="button" className="btn-secondary px-3 py-1 text-xs" disabled={remove.isPending} onClick={() => remove.mutate()}>
            Delete file
          </button>
        </div>
      )}
      {(st?.state === 'none' || st?.state === 'failed') && (
        <button type="button" className="btn-secondary px-3 py-1 text-xs" disabled={start.isPending} onClick={() => start.mutate()}>
          {st.state === 'failed' ? 'Try again' : 'Export'}
        </button>
      )}
      {st?.state === 'exporting' && (
        <button type="button" className="btn-secondary px-3 py-1 text-xs" disabled={remove.isPending} onClick={() => remove.mutate()}>
          Cancel
        </button>
      )}
      {st?.state === 'ready' && st.instructions && (
        <details className="text-xs text-ink-muted">
          <summary className="cursor-pointer">Instructions to use as the system prompt</summary>
          <pre className="log-panel mt-2 whitespace-pre-wrap">{st.instructions}</pre>
        </details>
      )}
      {error && <p className="text-sm text-danger">{errorText(error)}</p>}
    </div>
  )
}
