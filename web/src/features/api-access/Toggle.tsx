/** An on/off switch with an accessible name. */
export function Toggle({
  label,
  checked,
  disabled,
  busy,
  onChange,
}: {
  label: string
  checked: boolean
  disabled?: boolean
  /** Saving: the switch shows where it's going, and says it's busy. */
  busy?: boolean
  onChange: () => void
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      aria-busy={busy || undefined}
      disabled={disabled}
      onClick={onChange}
      className={[
        'relative inline-flex h-7 w-12 shrink-0 rounded-full transition disabled:opacity-50',
        busy ? 'animate-pulse' : '',
        checked ? 'bg-primary' : 'bg-raised',
      ].join(' ')}
    >
      <span
        className={[
          'absolute top-0.5 h-6 w-6 rounded-full bg-[#EEF2F6] shadow transition',
          checked ? 'start-[22px]' : 'start-0.5',
        ].join(' ')}
      />
    </button>
  )
}
