import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'

/**
 * Every cache with what it keeps, for how long, what clears it, and how
 * private it is (spec §36). In-memory caches can be cleared here.
 */
export function CachePanel() {
  const queryClient = useQueryClient()
  const query = useQuery({ queryKey: ['caches'], queryFn: () => api.listCaches(), retry: false, refetchInterval: 15_000 })
  const clear = useMutation({
    mutationFn: (name: string) => api.clearCache(name),
    onSettled: () => void queryClient.invalidateQueries({ queryKey: ['caches'] }),
  })
  const caches = query.data ?? []
  if (caches.length === 0) return null
  return (
    <section className="card space-y-3">
      <div>
        <h2 className="section-title">Caches</h2>
        <p className="mt-1 text-sm text-ink-muted">
          What Yggdrasil keeps to answer repeats faster. Personal caches stay on this computer and are cleared with run
          records. Passwords and keys are never cached.
        </p>
      </div>
      <ul className="divide-y divide-line/50">
        {caches.map((c) => (
          <li key={c.name} className="flex flex-wrap items-start justify-between gap-3 py-2.5 text-sm">
            <div className="min-w-0 flex-1">
              <p className="text-ink">
                {c.label} <span className="text-xs text-ink-faint">· {c.privacy}</span>
              </p>
              <p className="text-xs text-ink-muted">
                By {c.key}; {c.persistent ? 'kept until invalidated' : `kept ${c.ttl}`}
                {c.invalidation ? `; cleared when ${c.invalidation}` : ''}. {c.scope}.
              </p>
              <p className="text-xs text-ink-faint">
                {c.entries} {c.entries === 1 ? 'entry' : 'entries'}
                {c.persistent ? '' : ` · ${c.hits} hits · ${c.misses} misses`}
              </p>
            </div>
            {!c.persistent && (
              <button
                type="button"
                className="btn-secondary px-3 py-1 text-xs"
                disabled={clear.isPending || c.entries === 0}
                onClick={() => clear.mutate(c.name)}
              >
                Clear
              </button>
            )}
          </li>
        ))}
      </ul>
    </section>
  )
}
