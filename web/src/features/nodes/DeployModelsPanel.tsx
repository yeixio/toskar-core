import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { Model, Node } from '@/types/api'
import { formatBytes } from '@/lib/format'
import { fitLabelText } from '@/features/models/modelPresentation'

const fitOrder: Record<string, number> = { excellent: 0, good: 1, tight: 2, heavy: 3, too_large: 3, unsupported: 4 }

/** Where a model sorts by how well it fits this computer; unknown last. */
function fitRank(m: Model): number {
  return fitOrder[m.fit?.label ?? ''] ?? 5
}

export type DiskConflict = {
  node: Node
  available: number
  needed: number
}

function diskAvailable(node: Node): number | null {
  const avail = node.hardware?.disk?.available_bytes
  if (avail == null || avail < 0) return null
  return avail
}

/** Extra headroom so download temp + final fit comfortably. */
function neededBytes(size?: number): number {
  if (!size || size <= 0) return 512 * 1024 * 1024
  return Math.ceil(size * 1.05)
}

export function findDiskConflicts(
  model: Model | undefined,
  targets: Node[],
): DiskConflict[] {
  if (!model) return []
  const need = neededBytes(model.size_bytes)
  const conflicts: DiskConflict[] = []
  for (const node of targets) {
    const avail = diskAvailable(node)
    if (avail == null) continue
    if (avail < need) {
      conflicts.push({ node, available: avail, needed: need })
    }
  }
  return conflicts
}

export function DeployModelsPanel({
  models,
  nodes,
  busy,
  onDeploy,
}: {
  models: Model[]
  nodes: Node[]
  busy: boolean
  onDeploy: (modelId: string, nodeIds: string[]) => void
}) {
  const { t } = useTranslation('computers')
  const [modelId, setModelId] = useState('')
  const [selected, setSelected] = useState<Record<string, boolean>>({})
  const [forceDeploy, setForceDeploy] = useState(false)

  const online = useMemo(
    () => nodes.filter((n) => n.is_local || (n.paired && n.status !== 'offline')),
    [nodes],
  )

  const model = models.find((m) => m.id === modelId)

  const targets = useMemo(() => {
    return online.filter((n) => {
      if (!selected[n.id]) return false
      if (!model) return true
      return !(model.installed_on ?? []).some((p) => p.node_id === n.id)
    })
  }, [online, selected, model])

  const alreadyOn = useMemo(() => {
    if (!model) return []
    return online.filter((n) =>
      (model.installed_on ?? []).some((p) => p.node_id === n.id),
    )
  }, [model, online])

  const conflicts = useMemo(
    () => findDiskConflicts(model, targets),
    [model, targets],
  )

  // What fits this computer first, then by size, and never the test
  // placeholder (#1247).
  const selectable = useMemo(
    () =>
      models
        .filter((m) => m.family !== 'stub' && (m.source?.url || m.installed || (m.installed_on?.length ?? 0) > 0))
        .sort((a, b) => fitRank(a) - fitRank(b) || (a.size_bytes ?? 0) - (b.size_bytes ?? 0)),
    [models],
  )

  function optionLabel(m: Model): string {
    const name = m.size_bytes
      ? t('deploy.modelSize', { model: m.display_name, size: formatBytes(m.size_bytes) })
      : m.display_name
    const fit = fitLabelText(m.fit?.label)
    return fit ? t('deploy.modelFit', { model: name, fit }) : name
  }

  function toggle(nodeId: string) {
    setForceDeploy(false)
    setSelected((prev) => ({ ...prev, [nodeId]: !prev[nodeId] }))
  }

  function selectAllMissing() {
    setForceDeploy(false)
    const next: Record<string, boolean> = {}
    for (const n of online) {
      const has = model
        ? (model.installed_on ?? []).some((p) => p.node_id === n.id)
        : false
      next[n.id] = !has
    }
    setSelected(next)
  }

  const canDeploy =
    Boolean(modelId) &&
    targets.length > 0 &&
    (conflicts.length === 0 || forceDeploy) &&
    !busy

  return (
    <section className="card space-y-4 animate-fade">
      <div>
        <h2 className="font-display text-lg font-semibold text-ink">{t('deploy.title')}</h2>
        <p className="mt-1 text-sm text-ink-muted">{t('deploy.description')}</p>
      </div>

      <div className="grid gap-4 sm:grid-cols-2">
        <label className="block space-y-1.5 text-sm">
          <span className="text-ink-muted">{t('deploy.model')}</span>
          <select
            className="field w-full"
            value={modelId}
            onChange={(e) => {
              setModelId(e.target.value)
              setForceDeploy(false)
              setSelected({})
            }}
          >
            <option value="">{t('deploy.chooseModel')}</option>
            {selectable.map((m) => (
              <option key={m.id} value={m.id}>
                {optionLabel(m)}
              </option>
            ))}
          </select>
        </label>

        <div className="space-y-1.5 text-sm">
          <div className="flex items-center justify-between gap-2">
            <span className="text-ink-muted">{t('deploy.computers')}</span>
            {modelId && online.length > 1 && (
              <button
                type="button"
                className="text-xs text-primary hover:underline"
                onClick={selectAllMissing}
              >
                {t('deploy.selectMissing')}
              </button>
            )}
          </div>
          <ul className="max-h-40 space-y-1 overflow-y-auto rounded-xl bg-raised/50 p-2">
            {online.map((n) => {
              const has = model
                ? (model.installed_on ?? []).some((p) => p.node_id === n.id)
                : false
              const avail = diskAvailable(n)
              return (
                <li key={n.id}>
                  <label
                    className={[
                      'flex cursor-pointer items-center gap-2 rounded-lg px-2 py-1.5',
                      has ? 'opacity-60' : 'hover:bg-surface',
                    ].join(' ')}
                  >
                    <input
                      type="checkbox"
                      className="rounded border-line"
                      checked={Boolean(selected[n.id]) && !has}
                      disabled={has || !modelId}
                      onChange={() => toggle(n.id)}
                    />
                    <span className="min-w-0 flex-1 truncate text-ink">
                      {n.is_local ? t('deploy.thisComputer', { name: n.name }) : n.name}
                    </span>
                    <span className="shrink-0 text-xs text-ink-faint tabular-nums">
                      {has ? t('deploy.installed') : avail != null ? t('deploy.free', { size: formatBytes(avail) }) : '—'}
                    </span>
                  </label>
                </li>
              )
            })}
          </ul>
        </div>
      </div>

      {alreadyOn.length > 0 && model && (
        <p className="text-xs text-ink-faint">
          {t('deploy.alreadyOn', { names: alreadyOn.map((n) => n.name).join(', ') })}
        </p>
      )}

      {conflicts.length > 0 && (
        <div
          className="rounded-lg bg-danger/10 px-4 py-3 text-sm text-danger"
          role="alert"
        >
          <p className="font-medium">{t('deploy.notEnoughDisk')}</p>
          <ul className="mt-2 list-inside list-disc space-y-1">
            {conflicts.map((c) => (
              <li key={c.node.id}>
                {t('deploy.conflict', {
                  name: c.node.name,
                  needed: formatBytes(c.needed),
                  available: formatBytes(c.available),
                })}
              </li>
            ))}
          </ul>
          <label className="mt-3 flex items-center gap-2 text-ink">
            <input
              type="checkbox"
              className="rounded border-line"
              checked={forceDeploy}
              onChange={(e) => setForceDeploy(e.target.checked)}
            />
            <span className="text-xs">{t('deploy.tryAnyway')}</span>
          </label>
        </div>
      )}

      <div className="flex flex-wrap items-center gap-3">
        <button
          type="button"
          className="btn-primary"
          disabled={!canDeploy}
          onClick={() => {
            if (!modelId || targets.length === 0) return
            onDeploy(
              modelId,
              targets.map((target) => target.id),
            )
          }}
        >
          {busy
            ? t('deploy.deploying')
            : targets.length > 1
              ? t('deploy.deployTo', { count: targets.length })
              : targets.length === 1
                ? t('deploy.deployToName', { name: targets[0].name })
                : t('deploy.deploy')}
        </button>
        {model?.size_bytes ? (
          <span className="text-xs text-ink-faint">
            {t('deploy.downloadSize', { size: formatBytes(model.size_bytes) })}
          </span>
        ) : null}
      </div>
    </section>
  )
}
