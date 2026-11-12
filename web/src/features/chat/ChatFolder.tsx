import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { folderName } from './outputText'

/**
 * The folder a chat works in (#1017). Choosing one lets work asked for here
 * run commands there until it's done. A chat in a project with a folder
 * works in the project's unless it chooses its own (#1136).
 */
export function ChatFolder({
  folder,
  projectFolder = '',
  disabled,
  onChange,
}: {
  /** The chat's own folder, "" for none. */
  folder: string
  /** The folder of the project the chat is in, used when it has none of its own. */
  projectFolder?: string
  disabled?: boolean
  /** Sets it, "" for none. A rejection's message is shown here. */
  onChange: (folder: string) => Promise<void>
}) {
  const { t } = useTranslation('chat')
  const [open, setOpen] = useState(false)
  const [value, setValue] = useState(folder)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const change = async (next: string) => {
    setBusy(true)
    setError(null)
    try {
      await onChange(next)
      setValue(next)
      setOpen(false)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  const fromProject = !folder && projectFolder ? projectFolder : ''
  const inUse = folder || fromProject
  return (
    <div className="w-full">
      <button
        type="button"
        className={['composer-select text-xs', inUse ? 'text-ink' : 'text-ink-faint'].join(' ')}
        aria-expanded={open}
        disabled={disabled}
        title={inUse || t('folder.hint')}
        onClick={() => {
          setValue(folder)
          setError(null)
          setOpen((current) => !current)
        }}
      >
        <span className={['me-1.5 inline-block h-1.5 w-1.5 rounded-full', inUse ? 'bg-norn' : 'bg-line'].join(' ')} aria-hidden />
        {folder
          ? t('folder.on', { name: folderName(folder) })
          : fromProject
            ? t('folder.fromProject', { name: folderName(fromProject) })
            : t('folder.off')}
      </button>
      {open ? (
        // Not a form: this sits inside the composer's.
        <div
          className="mt-2 space-y-2"
          onKeyDown={(event) => {
            if (event.key !== 'Enter') return
            event.preventDefault()
            event.stopPropagation()
            if (value.trim()) void change(value.trim())
          }}
        >
          <label className="block text-xs text-ink-muted">
            {t('folder.label')}
            <input
              className="input mt-1 w-full font-mono text-xs"
              value={value}
              disabled={busy}
              autoFocus
              onFocus={(event) => event.target.select()}
              spellCheck={false}
              autoCapitalize="off"
              autoCorrect="off"
              placeholder={t('folder.placeholder')}
              onChange={(event) => setValue(event.target.value)}
            />
          </label>
          {fromProject ? <p className="break-all text-xs text-ink-muted">{t('folder.projectHint', { path: fromProject })}</p> : null}
          <p className="text-xs text-ink-muted">{t('folder.hint')}</p>
          {error ? (
            <p className="text-xs text-danger" role="alert">
              {error}
            </p>
          ) : null}
          <div className="flex gap-2">
            <button type="button" className="btn-primary text-xs" disabled={busy || !value.trim()} onClick={() => void change(value.trim())}>
              {t('folder.save')}
            </button>
            {folder ? (
              <button type="button" className="btn-secondary text-xs" disabled={busy} onClick={() => void change('')}>
                {t('folder.remove')}
              </button>
            ) : null}
          </div>
        </div>
      ) : null}
    </div>
  )
}
