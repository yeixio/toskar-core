import { useState } from 'react'
import type { Model, ModelFit, ModelDownloadProgressPayload } from '@/types/api'
import { formatBytes } from '@/lib/format'
import { SmallModelNote } from './SmallModelNote'
import { useUIStore } from '@/stores/uiStore'
import {
  bestForLabel,
  bestMachineName,
  contextTokensLabel,
  fitLabelText,
  installAction,
  machineMemoryLabel,
  memoryLabel,
  modelToolAssessment,
  purposeChips,
  runtimeRangeLabel,
  speedLabel,
} from './modelPresentation'

export function ModelCard({
  model,
  fit,
  peerFits,
  winnerLabel,
  progress,
  installing,
  onInstall,
  alternative,
  onInstallAlternative,
}: {
  model: Model
  /** A larger model to suggest when this one is small. */
  alternative?: Model | null
  onInstallAlternative?: (id: string) => void
  fit?: ModelFit
  peerFits?: { nodeName: string; label: ModelFit['label'] }[]
  winnerLabel?: string
  progress?: ModelDownloadProgressPayload
  installing?: boolean
  onInstall: () => void
}) {
  const advancedMode = useUIStore((s) => s.advancedMode)
  const [detailsOpen, setDetailsOpen] = useState(false)
  const isDownloading = model.status === 'downloading' || Boolean(progress)
  const chips = purposeChips(model)
  const tools = modelToolAssessment(model)
  const purpose = model.summary?.split(/[.!?]/)[0]?.trim() || bestForLabel(model)
  const install = installAction(fit)
  const speed = speedLabel(fit)
  const runtime = runtimeRangeLabel(fit)
  const machine = machineMemoryLabel(fit)
  const peers = (peerFits ?? []).filter((peer) => peer.nodeName)

  return (
    <article className="card flex h-full min-w-0 flex-col overflow-hidden transition duration-150">
      <div className="flex min-w-0 items-start justify-between gap-2">
        <h3 className="min-w-0 font-display text-lg font-semibold tracking-tight text-ink">
          {model.display_name}
        </h3>
        {winnerLabel ? (
          <span className="badge-preferred shrink-0">★ {winnerLabel}</span>
        ) : null}
      </div>

      <p className="mt-1 line-clamp-2 text-sm text-ink-muted">{purpose}</p>

      <p className="mt-3 text-sm text-ink">
        <span className="font-medium">{fitLabelText(fit?.label) || 'Fit unknown'}</span>
        <span className="text-ink-faint"> · </span>
        <span className="tabular-nums">{memoryLabel(model)}</span>
        {speed ? (
          <>
            <span className="text-ink-faint"> · </span>
            <span className="tabular-nums">{speed}</span>
          </>
        ) : null}
      </p>
      {fit?.reason ? <p className="mt-1 text-xs text-ink-muted">{fit.reason}</p> : null}
      {fit?.runtime_warning ? (
        <p className="mt-1 text-xs text-ink">{fit.runtime_warning}</p>
      ) : null}

      <div className="mt-2 flex flex-wrap gap-1.5">
        {chips.map((tag) => (
          <span key={tag} className="status-chip bg-raised/80 text-ink-muted">
            {tag}
          </span>
        ))}
      </div>
      <p className="mt-2 text-xs text-ink-muted" title={tools.detail}>
        {tools.summary}
      </p>
      <SmallModelNote model={model} alternative={alternative} onInstallAlternative={onInstallAlternative} />

      {isDownloading && progress && (
        <div className="mt-3">
          <div className="h-2 overflow-hidden rounded-full bg-raised">
            <div
              className="h-full bg-primary transition-all duration-300"
              style={{ width: `${Math.min(progress.percent, 100)}%` }}
            />
          </div>
          <p className="mt-1 text-xs text-ink-muted">
            {progress.percent.toFixed(0)}% · {formatBytes(progress.bytes_downloaded)} /{' '}
            {formatBytes(progress.bytes_total)}
          </p>
        </div>
      )}

      <div className="mt-auto flex flex-wrap gap-2 pt-4">
        {!model.installed ? (
          <button
            type="button"
            className="btn-primary px-3 py-1.5 text-xs"
            disabled={installing || isDownloading || install.disabled}
            onClick={onInstall}
          >
            {isDownloading ? 'Downloading…' : install.label}
          </button>
        ) : (
          <span className="status-chip bg-success/15 text-success">Installed</span>
        )}
        <button
          type="button"
          className="btn-secondary px-3 py-1.5 text-xs"
          onClick={() => setDetailsOpen((v) => !v)}
        >
          {detailsOpen ? 'Hide details' : 'Details'}
        </button>
      </div>

      {detailsOpen && (
        <dl className="mt-4 space-y-1 border-t border-line/50 pt-3 text-xs text-ink-muted">
          <Row label="Model size" value={memoryLabel(model)} />
          <Row label="Quantization" value={fit?.quantization || model.variant || 'Unknown'} />
          {runtime ? <Row label="Estimated runtime memory" value={runtime} /> : null}
          <Row
            label="Configured context"
            value={fit?.context_tokens ? contextTokensLabel(fit.context_tokens) : '—'}
          />
          {machine ? <Row label="Machine memory" value={machine} /> : null}
          {fit?.available_memory_bytes ? (
            <Row label="Available now" value={formatBytes(fit.available_memory_bytes)} />
          ) : null}
          <Row label="Expected behavior" value={fitLabelText(fit?.label) || 'Unknown'} />
          {fit?.gpu_note ? <Row label="GPU" value={fit.gpu_note} /> : null}
          {fit?.recommendations && fit.recommendations.length > 0 ? (
            <div className="pt-2">
              <dt>Recommendations</dt>
              <dd className="mt-1 text-ink">
                <ul className="list-disc space-y-0.5 pl-4">
                  {fit.recommendations.map((item) => (
                    <li key={item}>{item}</li>
                  ))}
                </ul>
              </dd>
            </div>
          ) : null}
          {peers.length > 1 ? (
            <div className="pt-2">
              <dt>On your computers</dt>
              <dd className="mt-1 space-y-0.5 text-right text-ink">
                {peers.map((peer) => (
                  <p key={`${peer.nodeName}-${peer.label}`}>
                    {peer.nodeName} · {fitLabelText(peer.label)}
                  </p>
                ))}
                <p>Best available machine: {bestMachineName(peers)}</p>
              </dd>
            </div>
          ) : null}
          {advancedMode && (
            <>
              <Row label="Model family" value={model.family || '—'} />
              <Row label="Parameters" value={model.parameters || '—'} />
              <Row label="Format" value={model.source?.format || 'gguf'} />
              <Row label="Runtime" value={(model.runtime ?? ['llamacpp']).join(', ')} />
              <Row label="Model ID" value={model.id} />
            </>
          )}
        </dl>
      )}
    </article>
  )
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex min-w-0 justify-between gap-3">
      <dt className="shrink-0">{label}</dt>
      <dd className="min-w-0 break-anywhere text-right text-ink" title={value}>
        {value}
      </dd>
    </div>
  )
}
