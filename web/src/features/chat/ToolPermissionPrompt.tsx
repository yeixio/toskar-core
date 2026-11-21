import { useTranslation } from 'react-i18next'
import { shotsDuration } from './videoShots'

export type PendingToolPrompt = {
  requestId: string
  toolId: string
  reason?: string
  argsSummary: string
  rawArgs?: string
  command?: string
  /** No earlier answer covers this command, so "allow for session" isn't offered. */
  alwaysAsks?: boolean
  cantUndo?: boolean
  afterOutside?: boolean
  /** A longer video's shots, its length, and about how long it takes here (#790). */
  shots?: { prompt: string; seconds: number }[]
  style?: string
  videoSeconds?: number
  estimateMinutes?: number
}

/** What a tool wants to do, and the answers to it. */
export function ToolPermissionPrompt({
  tool,
  toolName,
  showRawArgs,
  deciding,
  onDecide,
  agent,
  titleId = 'tool-permission-title',
}: {
  tool: PendingToolPrompt
  toolName: string
  showRawArgs: boolean
  deciding: boolean
  onDecide: (allow: boolean, allowSession: boolean) => void
  /** A swarm agent asking, such as "Agent 2: tests" (#1135). Its session is its own chat. */
  agent?: string
  titleId?: string
}) {
  const { t } = useTranslation('chat')
  return (
    <>
      <div>
        {agent ? <p className="text-xs font-medium text-norn">{agent}</p> : null}
        <h2
          id={titleId}
          className="font-display text-lg font-semibold text-ink"
        >
          {t('tools.permissionTitle', { tool: toolName })}
        </h2>
        {!tool.alwaysAsks && (
          <p className="mt-1 text-sm text-ink-muted">{tool.reason || t('tools.permissionReason')}</p>
        )}
      </div>
      {tool.shots ? (
        <div className="space-y-2 text-sm">
          <p className="text-ink">
            {t('tools.shots.summary', { count: tool.shots.length, seconds: Math.round(tool.videoSeconds ?? tool.shots.reduce((sum, s) => sum + s.seconds, 0)) })}
          </p>
          <p className="text-ink-muted">
            {tool.estimateMinutes ? t('tools.shots.estimate', { time: shotsDuration(t, tool.estimateMinutes) }) : t('tools.shots.noEstimate')}
          </p>
          {tool.style ? <p className="text-ink-muted">{t('tools.shots.style', { style: tool.style })}</p> : null}
          <ol className="max-h-56 list-decimal space-y-1 overflow-y-auto ps-5 text-ink" aria-label={t('tools.shots.list')}>
            {tool.shots.map((shot, i) => (
              <li key={i}>
                {shot.prompt} <span className="text-ink-faint">· {t('tools.shots.seconds', { n: shot.seconds })}</span>
              </li>
            ))}
          </ol>
        </div>
      ) : tool.alwaysAsks && tool.command ? (
        <pre className="log-panel max-h-40 whitespace-pre-wrap break-words">{tool.command}</pre>
      ) : (
        <p className="break-words text-sm text-ink">{tool.argsSummary}</p>
      )}
      {tool.alwaysAsks && (
        <p className={tool.cantUndo ? 'text-sm font-medium text-danger' : 'text-sm text-ink-muted'}>
          {t(tool.cantUndo ? 'tools.cantUndo' : tool.afterOutside ? 'tools.afterOutside' : 'tools.alwaysAsks')}
        </p>
      )}
      {tool.cantUndo && tool.afterOutside && <p className="text-sm text-ink-muted">{t('tools.afterOutside')}</p>}
      {tool.toolId === 'terminal' && !tool.alwaysAsks && !agent && (
        <p className="text-sm text-ink-muted">{t('tools.sessionTrustsProject')}</p>
      )}
      {agent && !tool.alwaysAsks && <p className="text-sm text-ink-muted">{t('swarm.allowAgentHint', { agent })}</p>}
      {showRawArgs && tool.rawArgs && (
        <pre className="log-panel max-h-40">{tool.rawArgs}</pre>
      )}
      <div className="flex flex-wrap gap-2">
        <button
          type="button"
          className="btn-primary"
          disabled={deciding}
          onClick={() => onDecide(true, false)}
        >
          {t('tools.allowOnce')}
        </button>
        {!tool.alwaysAsks && (
          <button
            type="button"
            className="btn-secondary"
            disabled={deciding}
            onClick={() => onDecide(true, true)}
          >
            {agent ? t('swarm.allowAgent') : t('tools.allowSession')}
          </button>
        )}
        <button
          type="button"
          className="btn-secondary"
          data-autofocus
          disabled={deciding}
          onClick={() => onDecide(false, false)}
        >
          {t('tools.deny')}
        </button>
      </div>
    </>
  )
}
