import { useTranslation } from 'react-i18next'
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
import { CommunityScore, RateButton } from './ratings'
import { formatPercent } from '@/i18n/format'
import { rovingKeyDown, useMenu } from '@/lib/roving'
import { searchItems } from '@/lib/search'
import { ShownError, type Shown } from '@/components/ui/ShownError'
import { useUIStore } from '@/stores/uiStore'

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
  onSpeculation,
  onContextWindow,
  onInstallElsewhere,
  installingId,
  startingId,
  startError,
  onDiscover,
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
  /** Turns guessing ahead (speculative decoding) Auto or Off for a model (#835). */
  onSpeculation?: (id: string, speculation: 'auto' | 'off') => void
  /** Sets a model's context window in tokens, or 0 for Auto (#962). */
  onContextWindow?: (id: string, contextWindow: number) => void
  onInstallElsewhere?: (id: string) => void
  installingId?: string | null
  /** The model starting now, which may first set up llama.cpp (#610). */
  startingId?: string | null
  /** Why the last start failed, for its model. */
  startError?: (Shown & { id: string }) | null
  /** Opens Discover, the empty tab's next step. */
  onDiscover?: () => void
}) {
  const { t } = useTranslation('models')
  const advancedMode = useUIStore((s) => s.advancedMode)
  const [menuOpenId, setMenuOpenId] = useState<string | null>(null)
  const menuRef = useMenu(menuOpenId, () => setMenuOpenId(null))
  // Typos forgiven (#451); exact matches first.
  const installed = searchItems(
    models.filter((m) => m.installed || (m.installed_on?.length ?? 0) > 0 || m.status === 'downloading'),
    search,
    (m) => [m.display_name, m.id],
  )
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
      <div className="flex flex-wrap items-center gap-3">
        <p className="text-sm text-ink-muted">{t('installedTab.empty')}</p>
        {onDiscover && !search ? (
          <button type="button" className="btn-secondary btn-sm" onClick={onDiscover}>
            {t('tabs.discover')}
          </button>
        ) : null}
      </div>
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
            : t('installedTab.thisComputer')

        return (
          <li key={model.id} className="card relative min-w-0">
            <div className="flex flex-wrap items-start justify-between gap-3">
              <div className="min-w-0">
                <h2 className="font-display text-lg font-semibold text-ink">
                  {model.display_name}
                </h2>
                {tightModelIds?.has(model.id) ? (
                  <p
                    className="mt-1 text-xs text-ink-muted"
                    title={t('tightFit.hint')}
                  >
                    {t('tightFit.label')}
                  </p>
                ) : null}
                <p className="mt-1 text-sm text-ink-muted">
                  {live ? (
                    <span className="text-success">{t('installedTab.runningOn', { computer: live.node_name })}</span>
                  ) : dl ? (
                    <span>{t('installedTab.downloading', { percent: formatPercent(dl.percent / 100) })}</span>
                  ) : (
                    <span>
                      {t('installedTab.installedOn', { where })}
                      {model.size_bytes ? ` · ${formatBytes(model.size_bytes)}` : ''}
                    </span>
                  )}
                </p>
                <p className="mt-1 text-xs text-ink-faint">
                  {usedBy.length > 0
                    ? t('installedTab.usedBy', { profiles: usedBy.join(', ') })
                    : t('installedTab.lastUsed', { when: formatLastUsed(model.last_used_at) })}
                </p>
                <CommunityScore modelId={model.id} />
                <SmallModelNote model={model} alternative={alternative} onInstallAlternative={onInstall} />
              </div>

              <div className="flex flex-wrap items-center gap-1.5">
                {live ? (
                  <Link to="/chat" className="btn-primary btn-sm">
                    {t('installedTab.chat')}
                  </Link>
                ) : (
                  showManualControls &&
                  !dl && (
                    <button
                      type="button"
                      className="btn-primary btn-sm"
                      disabled={startingId === model.id}
                      onClick={() => onStart(model.id)}
                    >
                      {startingId === model.id ? t('installedTab.starting') : t('installedTab.start')}
                    </button>
                  )
                )}
                {missing.length > 0 && onInstallElsewhere && (
                  <button
                    type="button"
                    className="btn-secondary btn-sm"
                    disabled={installingId === model.id}
                    onClick={() => onInstallElsewhere(model.id)}
                  >
                    {installingId === model.id ? t('installedTab.installing') : t('installedTab.installElsewhere')}
                  </button>
                )}
                {!dl && <RateButton modelId={model.id} modelName={model.display_name} />}
                <div className="relative">
                  <button
                    type="button"
                    className="icon-button"
                    aria-label={t('installedTab.moreActions', { model: model.display_name })}
                    aria-expanded={menuOpenId === model.id}
                    onClick={(e: MouseEvent) => {
                      e.stopPropagation()
                      setMenuOpenId((id) => (id === model.id ? null : model.id))
                    }}
                  >
                    <svg viewBox="0 0 16 16" className="h-4 w-4" fill="currentColor" aria-hidden>
                      <circle cx="3.5" cy="8" r="1.2" />
                      <circle cx="8" cy="8" r="1.2" />
                      <circle cx="12.5" cy="8" r="1.2" />
                    </svg>
                  </button>
                  {menuOpenId === model.id && (
                    <div
                      className="menu"
                      ref={menuRef}
                      role="menu"
                      onKeyDown={rovingKeyDown}
                      onClick={(e) => e.stopPropagation()}
                    >
                      {live && showManualControls && (
                        <button
                          type="button"
                          role="menuitem"
                          className="menu-item"
                          onClick={() => {
                            setMenuOpenId(null)
                            onStop(model.id, live.instance_id)
                          }}
                        >
                          {t('installedTab.stop')}
                        </button>
                      )}
                      {advancedMode && model.speculation && onSpeculation && (
                        <button
                          type="button"
                          role="menuitemcheckbox"
                          aria-checked={model.speculation === 'auto'}
                          className="menu-item"
                          title={t('installedTab.guessAheadHint')}
                          onClick={() => {
                            setMenuOpenId(null)
                            onSpeculation(model.id, model.speculation === 'auto' ? 'off' : 'auto')
                          }}
                        >
                          <span aria-hidden className="inline-block w-4">
                            {model.speculation === 'auto' ? '✓' : ''}
                          </span>
                          {t('installedTab.guessAhead')}
                          {live ? <span className="block text-xs text-ink-muted">{t('installedTab.guessAheadNextStart')}</span> : null}
                        </button>
                      )}
                      {advancedMode && model.context_options && model.context_options.length > 0 && onContextWindow && (
                        <div role="group" aria-label={t('installedTab.contextWindow')} title={t('installedTab.contextWindowHint')}>
                          <p className="px-3 pt-2 text-xs text-ink-muted">
                            {t('installedTab.contextWindow')}
                            {live ? ` · ${t('installedTab.contextWindowNextMessage')}` : ''}
                          </p>
                          {[0, ...model.context_options.map((o) => o.tokens)].map((tokens) => {
                            const option = model.context_options?.find((o) => o.tokens === tokens)
                            const chosen = (model.context_choice ?? 0) === tokens
                            const wontFit = option?.fit === 'unsupported' || option?.fit === 'too_large'
                            const label =
                              tokens === 0
                                ? model.context_choice
                                  ? t('installedTab.contextAuto')
                                  : t('installedTab.contextAutoSize', { size: tokenSize(model.context_window ?? 0) })
                                : tokenSize(tokens)
                            const note = option && option.fit !== 'excellent' && option.fit !== 'good' ? t(`fit.${option.fit}`) : ''
                            return (
                              <button
                                key={tokens}
                                type="button"
                                role="menuitemradio"
                                aria-checked={chosen}
                                disabled={wontFit}
                                className="menu-item"
                                onClick={() => {
                                  setMenuOpenId(null)
                                  if (!chosen) onContextWindow(model.id, tokens)
                                }}
                              >
                                <span aria-hidden className="inline-block w-4">
                                  {chosen ? '✓' : ''}
                                </span>
                                {note ? `${label} · ${note}` : label}
                              </button>
                            )
                          })}
                        </div>
                      )}
                      <button
                        type="button"
                        role="menuitem"
                        className="menu-item menu-item-danger"
                        onClick={() => {
                          setMenuOpenId(null)
                          onDelete(model.id)
                        }}
                      >
                        {t('installedTab.remove')}
                      </button>
                    </div>
                  )}
                </div>
              </div>
            </div>
            {startError?.id === model.id ? (
              <ShownError {...startError} role="alert" className="mt-2 text-sm text-danger" />
            ) : null}
          </li>
        )
      })}
    </ul>
  )
}

/** A context window as people say it: 32768 tokens is 32K. */
function tokenSize(tokens: number): string {
  return tokens >= 1024 ? `${Math.round(tokens / 1024)}K` : String(tokens)
}
