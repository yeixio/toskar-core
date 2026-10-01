import { useMutation } from '@tanstack/react-query'
import { useState } from 'react'
import { api } from '@/lib/api'
import type { KnowledgeRemoteInput } from '@/types/api'
import { errorText } from '@/features/train/display'

type Driver = NonNullable<KnowledgeRemoteInput['driver']>

const refreshChoices = [
  { minutes: 5, label: '5 minutes' },
  { minutes: 15, label: '15 minutes' },
  { minutes: 60, label: '1 hour' },
  { minutes: 360, label: '6 hours' },
  { minutes: 1440, label: '1 day' },
]

// RemoteSourceForm connects a database query or a web API as knowledge.
// Passwords and tokens go to the daemon's secrets directory and are never
// shown again.
export function RemoteSourceForm({ kind, onAdded }: { kind: 'database' | 'api'; onAdded: () => void }) {
  const [name, setName] = useState('')
  const [driver, setDriver] = useState<Driver>('sqlite')
  const [database, setDatabase] = useState('')
  const [connection, setConnection] = useState('')
  const [query, setQuery] = useState('')
  const [url, setUrl] = useState('')
  const [items, setItems] = useState('')
  const [headerName, setHeaderName] = useState('Authorization')
  const [headerValue, setHeaderValue] = useState('')
  const [refresh, setRefresh] = useState(60)

  const remote = (): KnowledgeRemoteInput =>
    kind === 'database'
      ? {
          driver,
          ...(driver === 'sqlite' ? { database } : { connection_string: connection }),
          query,
          refresh_minutes: refresh,
        }
      : {
          url,
          items: items || undefined,
          headers: headerValue ? { [headerName]: headerValue } : undefined,
          refresh_minutes: refresh,
        }
  const add = useMutation({
    mutationFn: () => api.createKnowledge({ kind, name: name || undefined, remote: remote() }),
    // A source whose first fetch failed is still added, with its error, so
    // the list refreshes either way.
    onSuccess: (src) => {
      if (src?.status !== 'failed') {
        setConnection('')
        setHeaderValue('')
      }
      onAdded()
    },
  })
  const ready =
    kind === 'database' ? query.trim() !== '' && (driver === 'sqlite' ? database.trim() !== '' : connection.trim() !== '') : url.trim() !== ''

  return (
    <>
      {kind === 'database' ? (
        <>
          <label className="block space-y-1">
            <span className="text-sm text-ink">Database</span>
            <select className="field w-full" value={driver} onChange={(e) => setDriver(e.target.value as Driver)}>
              <option value="sqlite">SQLite file</option>
              <option value="postgres">PostgreSQL</option>
              <option value="mysql">MySQL</option>
            </select>
          </label>
          {driver === 'sqlite' ? (
            <label className="block space-y-1">
              <span className="text-sm text-ink">File on this computer</span>
              <input className="field w-full font-mono text-xs" value={database} onChange={(e) => setDatabase(e.target.value)} placeholder="~/shop/inventory.db" />
            </label>
          ) : (
            <label className="block space-y-1">
              <span className="text-sm text-ink">Connection string</span>
              <input
                className="field w-full font-mono text-xs"
                type="password"
                autoComplete="off"
                value={connection}
                onChange={(e) => setConnection(e.target.value)}
                placeholder={driver === 'postgres' ? 'postgres://reader:password@db.local/shop' : 'reader:password@tcp(db.local:3306)/shop'}
              />
              <span className="block text-xs text-ink-faint">Use an account that can only read. It is stored apart from the database and not shown again.</span>
            </label>
          )}
          <label className="block space-y-1">
            <span className="text-sm text-ink">Query</span>
            <textarea
              className="field min-h-20 w-full font-mono text-xs"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="SELECT sku, name, price, in_stock FROM products WHERE active"
              aria-label="Query"
            />
            <span className="block text-xs text-ink-faint">One SELECT. It runs read-only, so Yggdrasil never changes the database. Each row becomes a passage.</span>
          </label>
        </>
      ) : (
        <>
          <label className="block space-y-1">
            <span className="text-sm text-ink">URL</span>
            <input className="field w-full font-mono text-xs" value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://shop.example.com/api/products" />
            <span className="block text-xs text-ink-faint">JSON, CSV, or text. A list of objects becomes one passage per item.</span>
          </label>
          <label className="block space-y-1">
            <span className="text-sm text-ink">List in the response (optional)</span>
            <input className="field w-full font-mono text-xs" value={items} onChange={(e) => setItems(e.target.value)} placeholder="data.products" />
          </label>
          <div className="grid grid-cols-[minmax(0,2fr)_minmax(0,3fr)] gap-2">
            <label className="block space-y-1">
              <span className="text-sm text-ink">Header</span>
              <input className="field w-full font-mono text-xs" value={headerName} onChange={(e) => setHeaderName(e.target.value)} />
            </label>
            <label className="block space-y-1">
              <span className="text-sm text-ink">Value (optional)</span>
              <input
                className="field w-full font-mono text-xs"
                type="password"
                autoComplete="off"
                value={headerValue}
                onChange={(e) => setHeaderValue(e.target.value)}
                placeholder="Bearer …"
              />
            </label>
          </div>
        </>
      )}
      <label className="block space-y-1">
        <span className="text-sm text-ink">Fetch again when older than</span>
        <select className="field w-full" value={refresh} onChange={(e) => setRefresh(Number(e.target.value))}>
          {refreshChoices.map((c) => (
            <option key={c.minutes} value={c.minutes}>
              {c.label}
            </option>
          ))}
        </select>
      </label>
      <label className="block space-y-1">
        <span className="text-sm text-ink">Name (optional)</span>
        <input className="field w-full" value={name} onChange={(e) => setName(e.target.value)} />
      </label>
      {add.error && <p className="text-sm text-danger">{errorText(add.error)}</p>}
      {add.data?.status === 'failed' && <p className="text-sm text-danger">{add.data.error}</p>}
      <button type="button" className="btn-primary px-3 py-1.5 text-sm" disabled={add.isPending || !ready} onClick={() => add.mutate()}>
        {add.isPending ? 'Connecting…' : 'Connect'}
      </button>
    </>
  )
}
