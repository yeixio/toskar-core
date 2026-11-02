import { useTranslation } from 'react-i18next'

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
}

/** What a tool wants to do, and the answers to it. */
export function ToolPermissionPrompt({
  tool,
  toolName,
  showRawArgs,
  deciding,
  onDecide,
}: {
  tool: PendingToolPrompt
  toolName: string
  showRawArgs: boolean
  deciding: boolean
  onDecide: (allow: boolean, allowSession: boolean) => void
}) {
  const { t } = useTranslation('chat')
  return (
    <>
      <div>
        <h2
          id="tool-permission-title"
          className="font-display text-lg font-semibold text-ink"
        >
          {t('tools.permissionTitle', { tool: toolName })}
        </h2>
        {!tool.alwaysAsks && (
          <p className="mt-1 text-sm text-ink-muted">{tool.reason || t('tools.permissionReason')}</p>
        )}
      </div>
      {tool.alwaysAsks && tool.command ? (
        <pre className="log-panel max-h-40 whitespace-pre-wrap break-words">{tool.command}</pre>
      ) : (
        <p className="break-words text-sm text-ink">{tool.argsSummary}</p>
      )}
      {tool.alwaysAsks && (
        <p className={tool.cantUndo ? 'text-sm font-medium text-danger' : 'text-sm text-ink-muted'}>
          {t(tool.cantUndo ? 'tools.cantUndo' : 'tools.alwaysAsks')}
        </p>
      )}
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
            {t('tools.allowSession')}
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
