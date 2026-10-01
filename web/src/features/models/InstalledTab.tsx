import { useEffect, useState, type MouseEvent } from 'react'
import { Link } from 'react-router-dom'
import type {
  AIProfile,
  Model,
  ModelDownloadProgressPayload,
  ModelFit,
  Node,
  RunningModelView,
} from '@/types/api'
import { formatBytes } from '@/lib/format'
import { formatLastUsed, largerAlternative } from './modelPresentation'
import { SmallModelNote } from './SmallModelNote'

export function InstalledTab({
  models,
  running,
  profiles,
  progress,
  nodes,
  showManualControls,
  search,
  tightModelIds,
  fits,
  onInstall,
  onStart,
  onStop,
  onDelete,
  onInstallElsewhere,
  installingId,
}: {
  models: Model[]
  running: RunningModelView[]
  profiles: AIProfile[]
  progress: Record<string, ModelDownloadProgressPayload>
  nodes: Node[]
  showManualControls: boolean
  search: string
  tightModelIds?: Set<string>
  /** Fit per model on this computer, to suggest a larger model. */
  fits?: Record<string, ModelFit>
  onInstall?: (id: string) => void
  onStart: (id: string) => void
  onStop: (id: string, instanceId: string) => void
  onDelete: (id: string) => void
  onInstallElsewhere?: (id: string) => void
  installingId?: string | null
}) {
  const [menuOpenId, setMenuOpenId] = useState<string | null>(null)
  const q = search.trim().toLowerCase()
  const installed = models.filter((m) => {
    const isInstalled =
      m.installed || (m.installed_on?.length ?? 0) > 0 || m.status === 'downloading'
    if (!isInstalled) return false
    if (!q) return true
    return (
      m.display_name.toLowerCase().includes(q) ||
      m.id.toLowerCase().includes(q)
    )
  })
  const runningByModel = new Map(running.map((r) => [r.model_id, r]))
  const alternative = largerAlternative(models, fits ?? {})
  const paired = nodes.filter((n) => (n.paired || n.is_local) && n.status !== 'offline')

  useEffect(() => {
    if (!menuOpenId) return
    const close = () => setMenuOpenId(null)
    window.addEventListener('click', close)
    return () => window.removeEventListener('click', close)
  }, [menuOpenId])

  if (installed.length === 0) {
    return (
      <p className="text-sm text-ink-muted">
        No installed models yet. Install from Discover, then they will appear here.
      </p>
    )
  }

  return (
    <ul className="space-y-3">
      {installed.map((model) => {
        const live = runningByModel.get(model.id)
        const usedBy = profiles
          .filter((p) => p.roles?.some((r) => r.model_id === model.id))
          .map((p) => p.name)
        const dl = progress[model.id]
        const onNodes = model.installed_on ?? []
        const missing =
          paired.length > 1
            ? paired.filter((n) => !onNodes.some((p) => p.node_id === n.id))
            : []
        const where =
          onNodes.length > 0
            ? onNodes.map((p) => p.node_name).join(', ')
            : 'This computer'

        return (
          <li key={model.id} className="card relative min-w-0">
            <div className="flex flex-wrap items-start justify-between gap-3">
              <div className="min-w-0">
                <h3 className="font-display text-lg font-semibold text-ink">
                  {model.display_name}
                </h3>
                {tightModelIds?.has(model.id) ? (
                  <p
                    className="mt-1 text-xs text-ink-muted"
                    title="Uses most of this computer's available memory and may be less stable."
                  >
                    Tight fit
                  </p>
                ) : null}
                <p className="mt-1 text-sm text-ink-muted">
                  {live ? (
                    <span className="text-success">Running on {live.node_name}</span>
                  ) : dl ? (
                    <span>Downloading… {dl.percent.toFixed(0)}%</span>
                  ) : (
                    <span>
                      Installed on {where}
                      {model.size_bytes ? ` · ${formatBytes(model.size_bytes)}` : ''}
                    </span>
                  )}
                </p>
                <p className="mt-1 text-xs text-ink-faint">
                  {usedBy.length > 0
                    ? `Used by ${usedBy.join(', ')}`
                    : `Last used ${formatLastUsed(model.last_used_at)}`}
                </p>
                <SmallModelNote model={model} alternative={alternative} onInstallAlternative={onInstall} />
              </div>

              <div className="flex flex-wrap items-center gap-2">
                {live ? (
                  <Link to="/chat" className="btn-primary px-3 py-1.5 text-xs">
                    Chat
                  </Link>
                ) : (
                  showManualControls &&
                  !dl && (
                    <button
                      type="button"
                      className="btn-primary px-3 py-1.5 text-xs"
                      onClick={() => onStart(model.id)}
                    >
                      Start
                    </button>
                  )
                )}
                {missing.length > 0 && onInstallElsewhere && (
                  <button
                    type="button"
                    className="btn-secondary px-3 py-1.5 text-xs"
                    disabled={installingId === model.id}
                    onClick={() => onInstallElsewhere(model.id)}
                  >
                    {installingId === model.id
                      ? 'Installing…'
                      : 'Install on other computers'}
                  </button>
                )}
                <div className="relative">
                  <button
                    type="button"
                    className="rounded-md px-2 py-1.5 text-xs text-ink-faint hover:bg-raised hover:text-ink"
                    aria-label={`More actions for ${model.display_name}`}
                    aria-expanded={menuOpenId === model.id}
                    onClick={(e: MouseEvent) => {
                      e.stopPropagation()
                      setMenuOpenId((id) => (id === model.id ? null : model.id))
                    }}
                  >
                    ···
                  </button>
                  {menuOpenId === model.id && (
                    <div
                      className="absolute right-0 top-full z-20 mt-1 min-w-[9rem] rounded-lg border border-line bg-surface py-1 shadow-panel"
                      role="menu"
                      onClick={(e) => e.stopPropagation()}
                    >
                      {live && showManualControls && (
                        <button
                          type="button"
                          role="menuitem"
                          className="block w-full px-3 py-1.5 text-left text-sm text-ink hover:bg-raised"
                          onClick={() => {
                            setMenuOpenId(null)
                            onStop(model.id, live.instance_id)
                          }}
                        >
                          Stop
                        </button>
                      )}
                      <button
                        type="button"
                        role="menuitem"
                        className="block w-full px-3 py-1.5 text-left text-sm text-danger hover:bg-danger/10"
                        onClick={() => {
                          setMenuOpenId(null)
                          onDelete(model.id)
                        }}
                      >
                        Remove
                      </button>
                    </div>
                  )}
                </div>
              </div>
            </div>
          </li>
        )
      })}
    </ul>
  )
}
