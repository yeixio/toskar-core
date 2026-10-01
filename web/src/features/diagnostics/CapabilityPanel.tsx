import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'

function bytesText(b: number): string {
  if (b >= 1 << 30) return `${(b / (1 << 30)).toFixed(1)} GB`
  if (b >= 1 << 20) return `${(b / (1 << 20)).toFixed(1)} MB`
  return `${Math.round(b / 1024)} KB`
}

/**
 * What Yggdrasil can do right now (spec §37): each ability, how it works
 * or what would make it possible, and what it is built from.
 */
export function CapabilityPanel() {
  const query = useQuery({ queryKey: ['capabilities'], queryFn: () => api.getCapabilities(), retry: false, staleTime: 15_000 })
  const snap = query.data
  if (!snap) return null
  const online = snap.nodes.filter((n) => n.online).length
  const tools = snap.tools.filter((t) => t.enabled).length
  const connected = snap.connectors.filter((c) => c.connected).length
  const healthy = snap.providers.filter((p) => p.healthy).length
  return (
    <section className="card space-y-3">
      <div>
        <h2 className="section-title">What Yggdrasil can do</h2>
        <p className="mt-1 text-sm text-ink-muted">
          From what is installed, online, and connected right now. Chat answers questions such as &ldquo;Can you
          generate an image?&rdquo; from this list.
        </p>
      </div>
      <ul className="grid gap-x-4 gap-y-1.5 text-sm sm:grid-cols-2">
        {snap.abilities.map((a) => (
          <li key={a.id} className="flex items-start gap-2">
            <span aria-hidden className={a.available ? 'text-success' : 'text-ink-faint'}>
              {a.available ? '✓' : '–'}
            </span>
            <span className="min-w-0">
              <span className={a.available ? 'text-ink' : 'text-ink-muted'}>{a.label}</span>
              <span className="sr-only">{a.available ? ' (available)' : ' (not available)'}</span>
              {(a.via?.length || a.note) && (
                <span className="block text-xs text-ink-faint">
                  {a.via?.length ? `Via ${a.via.join(', ')}. ` : ''}
                  {a.note}
                </span>
              )}
            </span>
          </li>
        ))}
      </ul>
      <p className="text-xs text-ink-faint">
        {snap.models.length} models installed · {online} of {snap.nodes.length} computers online · {tools} tools ·{' '}
        {connected} connected services · {healthy} of {snap.providers.length} providers ready · {snap.artifacts.count} files (
        {bytesText(snap.artifacts.bytes)})
      </p>
    </section>
  )
}
