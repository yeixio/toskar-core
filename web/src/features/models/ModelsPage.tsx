import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useMemo, useState } from 'react'
import { EmptyState } from '@/components/ui/EmptyState'
import { LoadingSpinner } from '@/components/ui/LoadingSpinner'
import { api } from '@/lib/api'
import { subscribeEvents } from '@/lib/events'
import { useUIStore } from '@/stores/uiStore'
import type { ModelDownloadProgressPayload, ModelFit, ModelsFitResponse, FitLabel } from '@/types/api'
import { BrowseAllPanel } from './BrowseAllPanel'
import { DiscoverTab } from './DiscoverTab'
import { InstalledTab } from './InstalledTab'
import {
  ModelsTargetBar,
  resolveInstallNodeId,
  type ModelsTarget,
} from './ModelsTargetBar'
import { needsTightFitInstallWarning } from './modelPresentation'
import { RunningTab } from './RunningTab'
import { TightFitDialog } from './TightFitDialog'

type Tab = 'discover' | 'installed' | 'running'

function pickFitBundle(
  bundles: ModelsFitResponse[] | undefined,
  target: ModelsTarget,
  localNodeId?: string,
): ModelsFitResponse | undefined {
  if (!bundles?.length) return undefined
  if (target === 'all' || target === 'local') {
    return (
      bundles.find((b) => b.node_id && localNodeId && b.node_id === localNodeId) ||
      bundles[0]
    )
  }
  return bundles.find((b) => b.node_id === target) || bundles[0]
}

export function ModelsPage() {
  const queryClient = useQueryClient()
  const advancedMode = useUIStore((s) => s.advancedMode)
  const [tab, setTab] = useState<Tab>('discover')
  const [search, setSearch] = useState('')
  const [browseOpen, setBrowseOpen] = useState(false)
  const [target, setTarget] = useState<ModelsTarget>('local')
  const [downloadProgress, setDownloadProgress] = useState<
    Record<string, ModelDownloadProgressPayload>
  >({})
  const [installingId, setInstallingId] = useState<string | null>(null)
  const [tightConfirm, setTightConfirm] = useState<{ id: string; name: string } | null>(null)

  const modelsQuery = useQuery({
    queryKey: ['models'],
    queryFn: () => api.getModels(),
    retry: false,
  })
  const fitQuery = useQuery({
    queryKey: ['models-fit'],
    queryFn: () => api.getModelsFit(),
    retry: false,
    staleTime: 60_000,
  })
  const hardwareQuery = useQuery({
    queryKey: ['hardware'],
    queryFn: () => api.getHardware(),
    retry: false,
    staleTime: 60_000,
  })
  const nodesQuery = useQuery({
    queryKey: ['nodes'],
    queryFn: () => api.getNodes(),
    retry: false,
  })
  const runningQuery = useQuery({
    queryKey: ['models-running'],
    queryFn: () => api.listRunningModels(),
    retry: false,
    refetchInterval: 5000,
  })
  const profilesQuery = useQuery({
    queryKey: ['profiles'],
    queryFn: () => api.getProfiles(),
    retry: false,
  })
  const settingsQuery = useQuery({
    queryKey: ['settings'],
    queryFn: () => api.getSettings(),
    retry: false,
  })

  useEffect(() => {
    const unsubscribe = subscribeEvents({
      onEvent: (event) => {
        if (event.type === 'model.download.progress') {
          const payload = event.payload as ModelDownloadProgressPayload | undefined
          if (payload?.model_id) {
            setDownloadProgress((current) => ({
              ...current,
              [payload.model_id]: payload,
            }))
          }
        }
        if (
          event.type === 'model.download.completed' ||
          event.type === 'model.download.failed' ||
          event.type === 'model.download.started'
        ) {
          const modelId = event.payload?.model_id as string | undefined
          if (modelId && event.type !== 'model.download.started') {
            setDownloadProgress((current) => {
              const next = { ...current }
              delete next[modelId]
              return next
            })
            setInstallingId(null)
          }
          queryClient.invalidateQueries({ queryKey: ['models'] })
          queryClient.invalidateQueries({ queryKey: ['models-running'] })
        }
      },
    })
    return unsubscribe
  }, [queryClient])

  const installMutation = useMutation({
    mutationFn: ({ id, nodeId }: { id: string; nodeId?: string }) =>
      api.installModel(id, { node_id: nodeId }),
    onMutate: ({ id }) => setInstallingId(id),
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ['models'] })
    },
  })
  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.deleteModel(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['models'] }),
  })
  const startMutation = useMutation({
    mutationFn: (id: string) => api.startModel(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['models-running'] }),
  })
  const stopMutation = useMutation({
    mutationFn: ({
      id,
      instanceId,
      nodeId,
    }: {
      id: string
      instanceId?: string
      nodeId?: string
    }) => api.stopModel(id, { instance_id: instanceId, node_id: nodeId }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['models-running'] }),
  })

  const models = modelsQuery.data ?? []
  const running = runningQuery.data ?? []
  const nodes = nodesQuery.data ?? []
  const profiles = profilesQuery.data ?? []
  const localNode = nodes.find((n) => n.is_local)
  const fitBundle = pickFitBundle(fitQuery.data ?? undefined, target, localNode?.id)
  const fits = useMemo(() => {
    const map: Record<string, ModelFit> = {}
    for (const f of fitBundle?.fits ?? []) {
      map[f.model_id] = f
    }
    return map
  }, [fitBundle])
  const peerFits = useMemo(() => {
    const map: Record<string, { nodeName: string; label: FitLabel }[]> = {}
    for (const bundle of fitQuery.data ?? []) {
      const nodeName = bundle.node_name || 'Computer'
      for (const fit of bundle.fits ?? []) {
        map[fit.model_id] = [
          ...(map[fit.model_id] ?? []),
          { nodeName: fit.node_name || nodeName, label: fit.label },
        ]
      }
    }
    return map
  }, [fitQuery.data])
  const tightModelIds = useMemo(() => {
    const ids = new Set<string>()
    for (const [id, fit] of Object.entries(fits)) {
      if (fit.label === 'tight') ids.add(id)
    }
    return ids
  }, [fits])
  const lifecycle = settingsQuery.data?.model_lifecycle ?? 'automatic'
  const showManualControls = advancedMode || lifecycle === 'manual'

  const installForPageTarget = (id: string) => {
    const model = models.find((item) => item.id === id)
    const fit = fits[id]
    if (model && needsTightFitInstallWarning(model, fit)) {
      setTightConfirm({ id, name: model.display_name })
      return
    }
    installMutation.mutate({ id, nodeId: resolveInstallNodeId(target) })
  }

  return (
    <div className="w-full min-w-0 space-y-6">
      <header className="page-header">
        <h1 className="page-title">Models</h1>
        <p className="page-subtitle">
          Choose what to install — Yggdrasil recommends models that fit your hardware.
        </p>
      </header>

      <ModelsTargetBar
        target={target}
        onTargetChange={setTarget}
        localHardware={hardwareQuery.data ?? null}
        nodes={nodes}
        running={running}
      />

      <div className="flex flex-wrap items-center gap-2">
        {(
          [
            { id: 'discover', label: 'Discover' },
            { id: 'installed', label: 'Installed' },
            { id: 'running', label: 'Running' },
          ] as const
        ).map((option) => (
          <button
            key={option.id}
            type="button"
            onClick={() => {
              setTab(option.id)
              if (option.id !== 'discover') setBrowseOpen(false)
            }}
            className={[
              'rounded-lg border px-4 py-2 text-sm font-medium transition',
              tab === option.id
                ? 'border-primary bg-primary-soft text-primary-active'
                : 'border-line bg-surface text-ink-muted hover:border-primary/40 hover:text-ink',
            ].join(' ')}
          >
            {option.label}
          </button>
        ))}
        {(tab === 'discover' || tab === 'installed') && (
          <input
            className="field ml-auto min-w-[200px] max-w-sm flex-1"
            placeholder="Search models…"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
        )}
      </div>

      {modelsQuery.isLoading && <LoadingSpinner label="Loading model library…" />}

      {!modelsQuery.isLoading && models.length === 0 && tab === 'discover' && (
        <EmptyState
          title="No curated models"
          description="The local catalog is empty. Try Browse all models to install from Hugging Face."
        />
      )}

      {browseOpen && tab === 'discover' && (
        <BrowseAllPanel
          installTarget={resolveInstallNodeId(target)}
          onClose={() => setBrowseOpen(false)}
          onInstalled={() => {
            queryClient.invalidateQueries({ queryKey: ['models'] })
          }}
        />
      )}

      {tab === 'discover' && models.length > 0 && (
        <DiscoverTab
          models={models}
          fits={fits}
          peerFits={peerFits}
          winners={fitBundle?.winners ?? []}
          progress={downloadProgress}
          search={search}
          installingId={installingId}
          onBrowseAll={() => setBrowseOpen(true)}
          onInstall={installForPageTarget}
        />
      )}

      {tab === 'installed' && (
        <InstalledTab
          models={models}
          running={running}
          profiles={profiles}
          progress={downloadProgress}
          nodes={nodes}
          showManualControls={showManualControls}
          search={search}
          installingId={installingId}
          tightModelIds={tightModelIds}
          fits={fits}
          onInstall={installForPageTarget}
          onInstallElsewhere={(id) => installMutation.mutate({ id, nodeId: 'all' })}
          onStart={(id) => startMutation.mutate(id)}
          onStop={(id, instanceId) => {
            const live = running.find((r) => r.model_id === id)
            stopMutation.mutate({ id, instanceId, nodeId: live?.node_id })
          }}
          onDelete={(id) => deleteMutation.mutate(id)}
        />
      )}

      {tab === 'running' && (
        <RunningTab
          running={running}
          profiles={profiles}
          localHardware={hardwareQuery.data ?? null}
          tightModelIds={tightModelIds}
          onStop={(modelId, instanceId, nodeId) =>
            stopMutation.mutate({ id: modelId, instanceId, nodeId })
          }
        />
      )}
      {tightConfirm ? (
        <TightFitDialog
          modelName={tightConfirm.name}
          onCancel={() => setTightConfirm(null)}
          onInstall={() => {
            const id = tightConfirm.id
            setTightConfirm(null)
            installMutation.mutate({ id, nodeId: resolveInstallNodeId(target) })
          }}
        />
      ) : null}
    </div>
  )
}
