import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

/** Memory On/Off for one chat. Off keeps saved memories out of it without deleting anything. */
export function MemoryToggle({
  memoryEnabled,
  off,
  disabled,
  onChange,
}: {
  memoryEnabled: boolean
  off: boolean
  disabled?: boolean
  onChange: (off: boolean) => void
}) {
  const { t } = useTranslation('chat')
  if (!memoryEnabled) {
    return (
      <Link to="/settings/memory" className="composer-select text-xs text-ink-faint" title={t('memory.disabledHint')}>
        {t('memory.off')}
      </Link>
    )
  }
  const on = !off
  return (
    <button
      type="button"
      className={['composer-select text-xs', on ? 'text-ink' : 'text-ink-faint'].join(' ')}
      aria-pressed={on}
      disabled={disabled}
      title={on ? t('memory.onHint') : t('memory.offHint')}
      onClick={() => onChange(on)}
    >
      <span className={['me-1.5 inline-block h-1.5 w-1.5 rounded-full', on ? 'bg-norn' : 'bg-line'].join(' ')} aria-hidden />
      {on ? t('memory.on') : t('memory.off')}
    </button>
  )
}
