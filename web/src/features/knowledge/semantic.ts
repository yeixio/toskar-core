import type { KnowledgeSource } from '@/types/api'

// meaningNote says whether a source can be searched by meaning, which needs
// an installed embedding model. Without one it returns null: search matches
// words, as always.
export function meaningNote(source: KnowledgeSource): string | null {
  const embedded = source.embedded_count ?? 0
  if (embedded === 0 || source.chunk_count === 0) return null
  if (embedded >= source.chunk_count) return 'Searchable by meaning, not only by matching words.'
  return `Searchable by meaning: ${embedded} of ${source.chunk_count} passages so far.`
}
