import { useLocation } from 'react-router-dom'
import { realmFor, runePaths, type RuneId } from '@/lib/realms'

/** An Elder Futhark rune, drawn as strokes in the current text color. */
export function Rune({ id, className = 'h-4 w-2.5' }: { id: RuneId; className?: string }) {
  return (
    <svg
      viewBox="0 0 10 16"
      className={['shrink-0 overflow-visible', className].join(' ')}
      fill="none"
      stroke="currentColor"
      strokeWidth={1.6}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden
    >
      <path d={runePaths[id]} />
    </svg>
  )
}

/**
 * The page's Norse name, set small above its title. It reads the current
 * route, so a page only has to place it.
 */
export function RealmKicker({ path, className = '' }: { path?: string; className?: string }) {
  const location = useLocation()
  const realm = realmFor(path ?? location.pathname)
  if (!realm) return null
  return (
    <p
      className={['realm-kicker', realm.accent, className].filter(Boolean).join(' ')}
      title={`${realm.meaning} ${realm.runeName}.`}
    >
      <Rune id={realm.rune} className="h-4 w-2.5" />
      <span>{realm.norse}</span>
    </p>
  )
}
