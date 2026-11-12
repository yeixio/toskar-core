import { Trans, useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

/**
 * The line under a chat's header that says which project it's in (#1136),
 * with Move to project and Remove from project. A new chat started from a
 * project's page says it will be in it.
 */
export function ChatProjectLine({
  projectId,
  name,
  draft,
  busy,
  onMove,
  onRemove,
}: {
  projectId: string
  /** The project's name, once it's loaded. */
  name?: string
  /** No chat yet: the next message starts one in the project. */
  draft?: boolean
  busy?: boolean
  onMove: () => void
  onRemove: () => void
}) {
  const { t } = useTranslation('chat')
  const link = (
    <Link
      to={`/projects/${encodeURIComponent(projectId)}`}
      className="font-medium text-ink underline-offset-2 hover:text-primary-active hover:underline"
    />
  )
  return (
    <div className="flex shrink-0 flex-wrap items-center justify-center gap-x-3 border-b border-line/40 px-4 py-0.5 text-xs text-ink-muted">
      <span className="flex min-w-0 items-center gap-1.5">
        <ProjectIcon />
        <span className="min-w-0 truncate">
          <Trans
            t={t}
            i18nKey={draft ? 'projectLine.newIn' : 'projectLine.in'}
            values={{ name: name ?? '…' }}
            components={{ go: link }}
          />
        </span>
      </span>
      {draft ? null : (
        <button type="button" className="inline-flex min-h-6 items-center text-ink-muted underline-offset-2 hover:text-ink hover:underline disabled:opacity-50" disabled={busy} onClick={onMove}>
          {t('projectLine.move')}
        </button>
      )}
      <button type="button" className="inline-flex min-h-6 items-center text-ink-muted underline-offset-2 hover:text-ink hover:underline disabled:opacity-50" disabled={busy} onClick={onRemove}>
        {t('projectLine.remove')}
      </button>
    </div>
  )
}

/** A folder with a tab: a project, in the history and above a chat. */
export function ProjectIcon({ className = 'h-3.5 w-3.5 shrink-0' }: { className?: string }) {
  return (
    <svg viewBox="0 0 16 16" className={className} fill="none" stroke="currentColor" strokeWidth={1.4} strokeLinejoin="round" aria-hidden>
      <path d="M2 4.5A1.5 1.5 0 0 1 3.5 3h2.6l1.4 1.5h5A1.5 1.5 0 0 1 14 6v5.5a1.5 1.5 0 0 1-1.5 1.5h-9A1.5 1.5 0 0 1 2 11.5z" />
    </svg>
  )
}
