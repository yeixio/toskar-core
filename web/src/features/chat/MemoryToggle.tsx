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
  if (!memoryEnabled) {
    return (
      <Link to="/memory" className="composer-select text-xs text-ink-faint" title="Memory is off for all chats. Turn it on from the Memory page.">
        Memory off
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
      title={
        on
          ? 'This chat uses what you asked Yggdrasil to remember. Click to keep memory out of this chat.'
          : 'Memory is off for this chat. Saved memories are not used here and are not deleted.'
      }
      onClick={() => onChange(on)}
    >
      <span className={['mr-1.5 inline-block h-1.5 w-1.5 rounded-full', on ? 'bg-norn' : 'bg-line'].join(' ')} aria-hidden />
      {on ? 'Memory on' : 'Memory off'}
    </button>
  )
}
