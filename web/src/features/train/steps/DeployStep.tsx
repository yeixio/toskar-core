import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { Ratatoskr } from '@/components/ui/Ratatoskr'
import { api } from '@/lib/api'
import { useMascotState } from '@/lib/ratatoskr/useMascotState'
import type { SpecializedAIView } from '@/types/api'
import { errorText } from '../display'
import { ExportCard } from './ExportCard'

export function DeployStep({ view }: { view: SpecializedAIView }) {
  const queryClient = useQueryClient()
  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['training'] })
  }
  const deploy = useMutation({ mutationFn: (rev: number) => api.deployRevision(view.id, rev), onSuccess: refresh })
  const undeploy = useMutation({ mutationFn: () => api.undeployAI(view.id), onSuccess: refresh })
  // Ratatoskr celebrates a deploy once, then stays beside it.
  const mascot = useMascotState({ react: [], celebrate: deploy.isSuccess ? deploy.submittedAt : undefined })

  return (
    <div className="space-y-4">
      <div className="card space-y-3">
        <div className="flex items-center gap-3">
          {deploy.isSuccess ? <Ratatoskr state={mascot} size={64} /> : null}
          <h3 className="section-title">Deploy</h3>
        </div>
        <p className="text-sm text-ink-muted">
          A deployed AI appears in the Chat model menu and in the API. It keeps its base model, adapter revision,
          instructions, and connected knowledge together. The base model stays available on its own.
        </p>
        {view.revisions.length === 0 && <p className="text-sm text-ink-muted">Train and test a revision first.</p>}
        <ul className="divide-y divide-line/60">
          {view.revisions.map((r) => {
            const deployed = r.revision === view.deployed_revision
            return (
              <li key={r.revision} className="flex flex-wrap items-center gap-3 py-2 text-sm">
                <div className="min-w-0 flex-1">
                  <p className="text-ink">
                    Revision {r.revision}
                    {deployed && <span className="status-chip ml-2 bg-success/15 text-success">Deployed</span>}
                  </p>
                  <p className="text-xs text-ink-muted">
                    {r.example_count} examples · {r.hyper.method?.toUpperCase()} · {new Date(r.created_at).toLocaleDateString()}
                    {r.final_val_loss != null && ` · validation loss ${r.final_val_loss.toFixed(3)}`}
                  </p>
                </div>
                {!r.evaluated ? (
                  <span className="text-xs text-ink-faint">Compare it first</span>
                ) : deployed ? (
                  <button type="button" className="btn-secondary px-3 py-1 text-xs" disabled={undeploy.isPending} onClick={() => undeploy.mutate()}>
                    Undeploy
                  </button>
                ) : (
                  <button type="button" className="btn-primary px-3 py-1 text-xs" disabled={deploy.isPending} onClick={() => deploy.mutate(r.revision)}>
                    {view.deployed_revision ? 'Switch to this revision' : 'Deploy'}
                  </button>
                )}
              </li>
            )
          })}
        </ul>
        {(deploy.error || undeploy.error) && <p className="text-sm text-danger">{errorText(deploy.error ?? undeploy.error)}</p>}
      </div>

      {view.deployed_revision > 0 && (
        <div className="card space-y-3">
          <h3 className="section-title">Use it</h3>
          <p className="text-sm text-ink-muted">
            In Chat, pick <span className="text-ink">{view.name}</span> from the model menu.{' '}
            <Link to="/chat?new=1" className="underline">
              Open Chat
            </Link>
            . From other programs, use the model id <span className="mono-id">{view.model_id}</span>:
          </p>
          <pre className="log-panel overflow-x-auto text-xs">{`curl http://127.0.0.1:7331/v1/chat/completions \\
  -H 'Content-Type: application/json' \\
  -d '{"model": "${view.model_id}", "messages": [{"role": "user", "content": "Hello"}]}'`}</pre>
          <p className="text-xs text-ink-faint">
            It runs on this computer, where its adapter is stored. Retraining creates a new revision; this one keeps
            answering until you deploy the new one.
          </p>
        </div>
      )}

      <ExportCard view={view} />
    </div>
  )
}
