import type { KnowledgeSource } from '@/types/api'

const driverNames = { sqlite: 'SQLite', postgres: 'PostgreSQL', mysql: 'MySQL' } as const

// sourceBadge labels how a source's content reaches Mimir.
export function sourceBadge(source: KnowledgeSource): string {
  switch (source.kind) {
    case 'path':
      return 'Linked'
    case 'database':
      return source.remote?.driver ? driverNames[source.remote.driver] : 'Database'
    case 'api':
      return 'Web API'
    default:
      return 'Copy'
  }
}

// sourceWhere says where a source's content comes from.
export function sourceWhere(source: KnowledgeSource): string {
  if (source.kind === 'path') return source.path ?? ''
  if (source.kind === 'database') return source.remote?.database ?? source.remote?.query ?? ''
  if (source.kind === 'api') return source.remote?.url ?? ''
  return source.filename ?? ''
}

// refreshNote explains when a database or API source is fetched again.
export function refreshNote(source: KnowledgeSource): string | null {
  const minutes = source.remote?.refresh_minutes
  if (!minutes) return null
  const every =
    minutes % 1440 === 0
      ? `${minutes / 1440} day${minutes === 1440 ? '' : 's'}`
      : minutes % 60 === 0
        ? `${minutes / 60} hour${minutes === 60 ? '' : 's'}`
        : `${minutes} minute${minutes === 1 ? '' : 's'}`
  return `Fetched again when a question uses it and the data is more than ${every} old, or on Reindex.`
}
