import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useMemo, useState, type MouseEvent } from 'react'
import { Link } from 'react-router-dom'
import { EmptyState } from '@/components/ui/EmptyState'
import { LoadingSpinner } from '@/components/ui/LoadingSpinner'
import { ApiError, api } from '@/lib/api'
import { formatBytes } from '@/lib/format'
import { useUIStore } from '@/stores/uiStore'
import type { Model, Node, PairingSession } from '@/types/api'
import { DeployModelsPanel } from './DeployModelsPanel'
import {
  availableForLabels,
  combinedMemoryBytes,
  describeNodeHardware,
  membershipKind,
  membershipLabel,
  onlineLabel,
  teamBestAt,
} from './nodePresentation'
import { RealmKicker } from '@/components/ui/Realm'

function errorMessage(err: unknown): string {
  if (err instanceof ApiError) return err.message
  if (err instanceof Error) return err.message
  return 'Something went wrong'
}

function modelsOnNode(models: Model[], nodeId: string): Model[] {
  return models.filter((m) =>
    (m.installed_on ?? []).some((p) => p.node_id === nodeId),
  )
}

function diskUsePercent(node: Node): number | null {
  const total = node.hardware?.disk?.total_bytes
  const avail = node.hardware?.disk?.available_bytes
  if (!total || total <= 0 || avail == null) return null
  const used = total - avail
  if (used < 0) return null
  return Math.min(100, Math.round((used / total) * 100))
}

function Badge({
  kind,
}: {
  kind: ReturnType<typeof membershipKind>
}) {
  const styles: Record<typeof kind, string> = {
    local: 'bg-primary/15 text-primary',
    paired: 'bg-accent/15 text-accent',
    nearby: 'bg-bifrost/15 text-bifrost',
    offline: 'bg-raised text-ink-muted',
  }
  return (
    <span className={['status-chip shrink-0', styles[kind]].join(' ')}>
      {membershipLabel(kind)}
    </span>
  )
}

function ComputerCard({
  node,
  runningCount,
  installedModels,
  claimCode,
  onClaimCode,
  onPair,
  onApproveCode,
  onRevoke,
  onRemoveModel,
  removingModelId,
  pairingBusy,
  claimBusy,
  revokeBusy,
}: {
  node: Node
  runningCount: number
  installedModels: Model[]
  claimCode: string
  onClaimCode: (code: string) => void
  onPair: () => void
  onApproveCode: () => void
  onRevoke: () => void
  onRemoveModel: (modelId: string) => void
  removingModelId: string | null
  pairingBusy: boolean
  claimBusy: boolean
  revokeBusy: boolean
}) {
  const advancedMode = useUIStore((s) => s.advancedMode)
  const [manageOpen, setManageOpen] = useState(false)
  const [menuOpen, setMenuOpen] = useState(false)
  const kind = membershipKind(node)
  const hw = describeNodeHardware(node.hardware)
  const capabilities = availableForLabels(installedModels, node.hardware)
  const diskPct = diskUsePercent(node)
  const diskFree = node.hardware?.disk?.available_bytes
  const diskTotal = node.hardware?.disk?.total_bytes
  const diskTight = diskPct != null && diskPct >= 90
  const online = onlineLabel(node)
  const isOnline = online === 'Online'
  const nearby = kind === 'nearby'
  const inTeam = kind === 'local' || kind === 'paired' || kind === 'offline'

  useEffect(() => {
    if (!menuOpen) return
    const close = () => setMenuOpen(false)
    window.addEventListener('click', close)
    return () => window.removeEventListener('click', close)
  }, [menuOpen])

  return (
    <article
      className={[
        'card flex h-full flex-col transition duration-150 animate-fade',
        nearby ? 'ring-1 ring-bifrost/40' : '',
        kind === 'local' ? 'ring-1 ring-primary/25' : '',
        diskTight && inTeam ? 'ring-1 ring-danger/40' : '',
        kind === 'offline' ? 'opacity-80' : '',
      ].join(' ')}
    >
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <h2 className="font-display text-lg font-semibold tracking-tight text-ink">
            {node.name}
          </h2>
          <p className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-sm text-ink-muted">
            <Badge kind={kind} />
            {kind !== 'offline' && (
              <>
                <span className="text-ink-faint">·</span>
                <span
                  className={[
                    'inline-flex items-center gap-1.5',
                    isOnline ? 'text-success' : 'text-ink-muted',
                  ].join(' ')}
                >
                  <span
                    className={[
                      'h-1.5 w-1.5 rounded-full',
                      isOnline ? 'bg-success' : 'bg-ink-faint',
                    ].join(' ')}
                    aria-hidden
                  />
                  {online}
                </span>
              </>
            )}
          </p>
        </div>
      </div>

      {nearby ? (
        <p className="mt-4 text-sm text-ink-muted">
          On your network and ready to join this team.
        </p>
      ) : (
        <div className="mt-4 space-y-3 text-sm">
          {hw.unavailable ? (
            <p className="text-ink-faint">Hardware details unavailable</p>
          ) : (
            <div className="text-ink-muted">
              {hw.primary ? <p className="text-ink">{hw.primary}</p> : null}
              {hw.secondary ? <p className="mt-0.5">{hw.secondary}</p> : null}
            </div>
          )}

          <p className="text-ink">
            Running:{' '}
            <span className="font-medium">
              {runningCount} {runningCount === 1 ? 'model' : 'models'}
            </span>
          </p>

          {capabilities.length > 0 && (
            <p className="text-sm text-ink-muted">
              <span className="text-ink-faint">Available for:</span>{' '}
              <span className="text-ink">{capabilities.join(', ')}</span>
            </p>
          )}

          {kind === 'paired' || kind === 'offline' ? (
            <p className="text-xs text-accent">Part of this team</p>
          ) : null}

          {advancedMode && node.address ? (
            <p className="truncate font-mono text-[11px] text-ink-faint">{node.address}</p>
          ) : null}
        </div>
      )}

      {manageOpen && inTeam && (
        <div className="mt-4 space-y-3 border-t border-line/60 pt-4 text-sm text-ink-muted animate-fade">
          {diskPct != null && diskFree != null && diskTotal != null && (
            <div>
              <div className="mb-1 flex justify-between text-xs">
                <span>Disk</span>
                <span
                  className={[
                    'tabular-nums',
                    diskTight ? 'font-medium text-danger' : 'text-ink',
                  ].join(' ')}
                >
                  {formatBytes(diskFree)} free of {formatBytes(diskTotal)}
                </span>
              </div>
              <div className="h-1.5 overflow-hidden rounded-full bg-raised">
                <div
                  className={[
                    'h-full transition-all duration-300',
                    diskTight ? 'bg-danger/80' : 'bg-accent/80',
                  ].join(' ')}
                  style={{ width: `${diskPct}%` }}
                />
              </div>
              {diskTight ? (
                <p className="mt-1 text-xs text-danger">Low free space for more models</p>
              ) : null}
            </div>
          )}

          <div>
            <p className="text-xs font-medium text-ink">
              Models on this computer
              {installedModels.length > 0 ? ` (${installedModels.length})` : ''}
            </p>
            {installedModels.length === 0 ? (
              <p className="mt-1 text-xs text-ink-faint">
                None yet. Deploy below or open Models.
              </p>
            ) : (
              <ul className="mt-2 space-y-1.5">
                {installedModels.map((m) => (
                  <li
                    key={m.id}
                    className="flex items-center justify-between gap-2 rounded-lg bg-raised/50 px-2.5 py-1.5"
                  >
                    <div className="min-w-0">
                      <p className="truncate text-xs font-medium text-ink">{m.display_name}</p>
                      {m.size_bytes ? (
                        <p className="text-[10px] text-ink-faint">{formatBytes(m.size_bytes)}</p>
                      ) : null}
                    </div>
                    <button
                      type="button"
                      className="btn-danger shrink-0 px-2 py-1 text-[10px]"
                      disabled={removingModelId === m.id}
                      onClick={() => onRemoveModel(m.id)}
                    >
                      {removingModelId === m.id ? 'Removing…' : 'Remove'}
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </div>
        </div>
      )}

      <div className="mt-auto flex flex-wrap items-center gap-2 pt-5">
        {nearby && (
          <>
            <button
              type="button"
              className="btn-primary"
              onClick={onPair}
              disabled={pairingBusy || !node.address}
              title={!node.address ? 'No address yet — try Find computers' : undefined}
            >
              {pairingBusy ? 'Connecting…' : 'Add to team'}
            </button>
            <div className="flex flex-wrap items-center gap-2">
              <input
                type="text"
                inputMode="numeric"
                maxLength={6}
                placeholder="Code"
                aria-label={`Pairing code for ${node.name}`}
                className="field w-24 px-2 py-1.5 text-xs font-mono"
                value={claimCode}
                onChange={(e) => onClaimCode(e.target.value.replace(/\D/g, '').slice(0, 6))}
              />
              <button
                type="button"
                className="btn-secondary px-3 py-1.5 text-xs"
                disabled={claimBusy || !node.address || claimCode.length !== 6}
                onClick={onApproveCode}
              >
                {claimBusy ? 'Approving…' : 'Approve with code'}
              </button>
            </div>
          </>
        )}

        {inTeam && (
          <>
            <button
              type="button"
              className="btn-primary px-3 py-1.5 text-xs"
              onClick={() => setManageOpen((o) => !o)}
              aria-expanded={manageOpen}
            >
              {manageOpen ? 'Hide details' : 'Manage'}
            </button>
            {!node.is_local && node.paired && (
              <div className="relative ml-auto">
                <button
                  type="button"
                  className="rounded-md px-2 py-1.5 text-xs text-ink-faint hover:bg-raised hover:text-ink"
                  aria-label={`More actions for ${node.name}`}
                  aria-expanded={menuOpen}
                  onClick={(e: MouseEvent) => {
                    e.stopPropagation()
                    setMenuOpen((o) => !o)
                  }}
                >
                  ···
                </button>
                {menuOpen && (
                  <div
                    className="absolute right-0 z-20 mt-1 min-w-[10rem] rounded-xl border border-line bg-surface py-1 shadow-lg"
                    onClick={(e) => e.stopPropagation()}
                  >
                    <button
                      type="button"
                      className="block w-full px-3 py-2 text-left text-xs text-danger hover:bg-danger/10"
                      disabled={revokeBusy}
                      onClick={() => {
                        setMenuOpen(false)
                        onRevoke()
                      }}
                    >
                      {revokeBusy ? 'Removing…' : 'Remove from team'}
                    </button>
                  </div>
                )}
              </div>
            )}
          </>
        )}
      </div>
    </article>
  )
}

export function NodesPage() {
  const queryClient = useQueryClient()
  const advancedMode = useUIStore((s) => s.advancedMode)
  const [pairingSession, setPairingSession] = useState<PairingSession | null>(null)
  const [claimCodes, setClaimCodes] = useState<Record<string, string>>({})
  const [removingKey, setRemovingKey] = useState<string | null>(null)

  const nodesQuery = useQuery({
    queryKey: ['nodes'],
    queryFn: () => api.getNodes(),
    retry: false,
    refetchInterval: 10_000,
  })

  const pendingQuery = useQuery({
    queryKey: ['pairing-pending'],
    queryFn: () => api.listPairingOffers(),
    retry: false,
    refetchInterval: 3000,
  })

  const runningQuery = useQuery({
    queryKey: ['models-running'],
    queryFn: () => api.listRunningModels(),
    retry: false,
    refetchInterval: 5000,
  })

  const modelsQuery = useQuery({
    queryKey: ['models'],
    queryFn: () => api.getModels(),
    retry: false,
    refetchInterval: 10_000,
  })

  const pairMutation = useMutation({
    mutationFn: (nodeId: string) => api.pairNode(nodeId),
    onSuccess: (session) => {
      if (session) setPairingSession(session)
      queryClient.invalidateQueries({ queryKey: ['nodes'] })
    },
  })

  const approveMutation = useMutation({
    mutationFn: (sessionId: string) => api.approvePairing(sessionId),
    onSuccess: () => {
      setPairingSession(null)
      queryClient.invalidateQueries({ queryKey: ['nodes'] })
      queryClient.invalidateQueries({ queryKey: ['pairing-pending'] })
    },
  })

  const claimMutation = useMutation({
    mutationFn: async ({ nodeId, code }: { nodeId: string; code: string }) => {
      const session = await api.claimPairing(nodeId, code)
      if (!session?.id) {
        throw new Error('Could not load pairing offer for that code')
      }
      return api.approvePairing(session.id)
    },
    onSuccess: () => {
      setClaimCodes({})
      setPairingSession(null)
      queryClient.invalidateQueries({ queryKey: ['nodes'] })
      queryClient.invalidateQueries({ queryKey: ['pairing-pending'] })
    },
  })

  const revokeMutation = useMutation({
    mutationFn: (nodeId: string) => api.revokeNode(nodeId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['nodes'] })
    },
  })

  const refreshMutation = useMutation({
    mutationFn: () => api.refreshNodes(),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['nodes'] })
    },
  })

  const deployMutation = useMutation({
    mutationFn: async ({ modelId, nodeIds }: { modelId: string; nodeIds: string[] }) => {
      const errors: string[] = []
      for (const nodeId of nodeIds) {
        try {
          await api.installModel(modelId, { node_id: nodeId })
        } catch (err) {
          errors.push(`${nodeId}: ${errorMessage(err)}`)
        }
      }
      if (errors.length > 0) {
        throw new Error(errors.join('; '))
      }
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['models'] })
      queryClient.invalidateQueries({ queryKey: ['nodes'] })
    },
  })

  const removeMutation = useMutation({
    mutationFn: async ({ modelId, nodeId }: { modelId: string; nodeId: string }) => {
      setRemovingKey(`${nodeId}:${modelId}`)
      try {
        await api.deleteModel(modelId, { node_id: nodeId })
      } finally {
        setRemovingKey(null)
      }
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['models'] })
      queryClient.invalidateQueries({ queryKey: ['models-running'] })
      queryClient.invalidateQueries({ queryKey: ['nodes'] })
    },
  })

  const nodes = nodesQuery.data ?? []
  const pending = pendingQuery.data ?? []
  const running = runningQuery.data ?? []
  const models = modelsQuery.data ?? []

  const nearby = useMemo(
    () => nodes.filter((n) => !n.is_local && !n.paired),
    [nodes],
  )
  const fleet = useMemo(
    () => nodes.filter((n) => n.is_local || n.paired),
    [nodes],
  )
  const pairedRemotes = nodes.filter((n) => !n.is_local && n.paired)

  const runningByNode = useMemo(() => {
    const map = new Map<string, number>()
    for (const r of running) {
      map.set(r.node_id, (map.get(r.node_id) ?? 0) + 1)
    }
    return map
  }, [running])

  const teamMem = useMemo(() => combinedMemoryBytes(fleet), [fleet])
  const teamCaps = useMemo(() => teamBestAt(fleet, models), [fleet, models])
  const totalRunning = running.length

  const teamNoun = advancedMode ? 'cluster' : 'team'
  const sectionTitle = advancedMode ? 'Your cluster' : 'Your Yggdrasil team'

  const actionError =
    pairMutation.error ??
    approveMutation.error ??
    claimMutation.error ??
    revokeMutation.error ??
    refreshMutation.error ??
    deployMutation.error ??
    removeMutation.error

  return (
    <div className="w-full min-w-0 space-y-8">
      <header className="page-header flex flex-wrap items-end justify-between gap-4">
        <div>
          <RealmKicker />
          <h1 className="page-title">
            {advancedMode ? 'Your AI cluster' : 'Your Yggdrasil team'}
          </h1>
          <p className="page-subtitle">
            These computers work together as one AI system. Add a machine, then deploy
            models where they fit.
          </p>
        </div>
        <button
          type="button"
          className="btn-primary"
          disabled={refreshMutation.isPending}
          onClick={() => refreshMutation.mutate()}
        >
          {refreshMutation.isPending ? 'Scanning…' : 'Find computers'}
        </button>
      </header>

      {fleet.length > 0 && (
        <section className="rounded-2xl bg-raised/40 px-5 py-4 animate-fade">
          <p className="label-caps text-[10px] text-ink-faint">
            {advancedMode ? 'Cluster status' : 'Team status'}
          </p>
          <p className="mt-1 text-sm text-ink">
            {fleet.length} {fleet.length === 1 ? 'computer' : 'computers'}
            {teamMem > 0 ? ` · ${formatBytes(teamMem)} combined memory` : ''}
            {` · ${totalRunning} ${totalRunning === 1 ? 'model' : 'models'} running`}
          </p>
          {teamCaps.length > 0 && (
            <p className="mt-2 text-sm text-ink-muted">
              <span className="text-ink-faint">Best at</span>{' '}
              <span className="text-ink">{teamCaps.join(' · ')}</span>
            </p>
          )}
        </section>
      )}

      {actionError && (
        <div className="rounded-lg bg-danger/10 px-4 py-3 text-sm text-danger">
          <p className="font-medium">Could not complete that action</p>
          <p className="mt-1">{errorMessage(actionError)}</p>
          {(pairMutation.error ||
            approveMutation.error ||
            claimMutation.error ||
            revokeMutation.error ||
            refreshMutation.error) && (
            <p className="mt-2 text-xs opacity-90">
              Allow Local Network / Firewall for Yggdrasil on both computers, and keep Find other
              computers On in Settings.
            </p>
          )}
        </div>
      )}

      {pairingSession && (
        <section className="card space-y-2 ring-1 ring-accent/40 animate-fade">
          <h2 className="font-display text-lg font-semibold text-ink">Waiting for approval</h2>
          <p className="text-sm text-ink">
            Pairing with{' '}
            <span className="font-medium">
              {pairingSession.remote_name || pairingSession.remote_node_id || 'the other computer'}
            </span>
            . On that computer, open Computers and Approve the incoming request.
          </p>
          <p className="text-sm text-ink-muted">
            Code:{' '}
            <span className="font-mono text-lg font-semibold text-ink">
              {pairingSession.code || '—'}
            </span>
          </p>
        </section>
      )}

      {pending.length > 0 && (
        <section className="card space-y-3 animate-fade">
          <h2 className="font-display text-lg font-semibold text-ink">Incoming requests</h2>
          <ul className="space-y-2">
            {pending.map((offer) => (
              <li
                key={offer.id}
                className="flex flex-wrap items-center justify-between gap-3 rounded-xl bg-raised/60 px-4 py-3"
              >
                <div>
                  <p className="font-medium text-ink">{offer.remote_name}</p>
                  <p className="text-xs text-ink-muted">
                    Code <span className="font-mono">{offer.code}</span>
                  </p>
                </div>
                <button
                  type="button"
                  className="btn-primary px-3 py-1.5 text-xs"
                  disabled={approveMutation.isPending}
                  onClick={() => approveMutation.mutate(offer.id)}
                >
                  Approve
                </button>
              </li>
            ))}
          </ul>
        </section>
      )}

      {nodesQuery.isLoading && <LoadingSpinner label="Looking for computers…" />}

      {!nodesQuery.isLoading && nodes.length === 0 && (
        <EmptyState
          title="Build your AI team"
          description="Install Yggdrasil on another computer on your network and it will appear here automatically."
          action={
            <button
              type="button"
              className="btn-primary"
              disabled={refreshMutation.isPending}
              onClick={() => refreshMutation.mutate()}
            >
              Find computers
            </button>
          }
        />
      )}

      {nearby.length > 0 && (
        <section className="space-y-3">
          <div>
            <h2 className="section-title">Available to add</h2>
            <p className="mt-1 text-sm text-ink-muted">
              Yggdrasil found these computers on your network.
            </p>
          </div>
          <ul className="grid gap-4 md:grid-cols-2">
            {nearby.map((node) => (
              <li key={node.id}>
                <ComputerCard
                  node={node}
                  runningCount={0}
                  installedModels={[]}
                  claimCode={claimCodes[node.id] ?? ''}
                  onClaimCode={(code) =>
                    setClaimCodes((prev) => ({ ...prev, [node.id]: code }))
                  }
                  onPair={() => pairMutation.mutate(node.id)}
                  onApproveCode={() =>
                    claimMutation.mutate({
                      nodeId: node.id,
                      code: claimCodes[node.id] ?? '',
                    })
                  }
                  onRevoke={() => revokeMutation.mutate(node.id)}
                  onRemoveModel={() => {}}
                  removingModelId={null}
                  pairingBusy={pairMutation.isPending}
                  claimBusy={claimMutation.isPending}
                  revokeBusy={revokeMutation.isPending}
                />
              </li>
            ))}
          </ul>
        </section>
      )}

      {fleet.length > 0 && (
        <section className="space-y-3">
          <div className="flex flex-wrap items-end justify-between gap-3">
            <div>
              <h2 className="section-title">{sectionTitle}</h2>
              <p className="mt-1 text-sm text-ink-muted">
                {pairedRemotes.length > 0
                  ? `Work moves across this ${teamNoun} automatically.`
                  : 'This is the only computer in the team so far.'}
              </p>
            </div>
            <div className="flex flex-wrap gap-2">
              <Link to="/models" className="btn-secondary px-3 py-1.5 text-xs">
                Install models
              </Link>
              <Link to="/chat" className="btn-secondary px-3 py-1.5 text-xs">
                Open Chat
              </Link>
            </div>
          </div>
          <ul className="grid gap-4 md:grid-cols-2">
            {fleet.map((node) => (
              <li key={node.id}>
                <ComputerCard
                  node={node}
                  runningCount={runningByNode.get(node.id) ?? 0}
                  installedModels={modelsOnNode(models, node.id)}
                  claimCode={claimCodes[node.id] ?? ''}
                  onClaimCode={(code) =>
                    setClaimCodes((prev) => ({ ...prev, [node.id]: code }))
                  }
                  onPair={() => pairMutation.mutate(node.id)}
                  onApproveCode={() =>
                    claimMutation.mutate({
                      nodeId: node.id,
                      code: claimCodes[node.id] ?? '',
                    })
                  }
                  onRevoke={() => revokeMutation.mutate(node.id)}
                  onRemoveModel={(modelId) =>
                    removeMutation.mutate({ modelId, nodeId: node.id })
                  }
                  removingModelId={
                    removingKey?.startsWith(`${node.id}:`)
                      ? removingKey.slice(node.id.length + 1)
                      : null
                  }
                  pairingBusy={pairMutation.isPending}
                  claimBusy={claimMutation.isPending}
                  revokeBusy={revokeMutation.isPending}
                />
              </li>
            ))}
          </ul>
        </section>
      )}

      {fleet.length > 0 && (
        <DeployModelsPanel
          models={models}
          nodes={fleet}
          busy={deployMutation.isPending}
          onDeploy={(modelId, nodeIds) =>
            deployMutation.mutate({ modelId, nodeIds })
          }
        />
      )}
    </div>
  )
}
