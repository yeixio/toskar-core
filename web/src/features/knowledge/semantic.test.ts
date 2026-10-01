import { describe, expect, it } from 'vitest'
import type { KnowledgeSource } from '@/types/api'
import { meaningNote } from './semantic'

const source = (chunks: number, embedded?: number): KnowledgeSource => ({
  id: 'k', name: 'faq', kind: 'text', status: 'ready', chunk_count: chunks, embedded_count: embedded,
  created_at: '', updated_at: '',
})

describe('meaningNote', () => {
  it('says nothing without an embedding model', () => {
    expect(meaningNote(source(10))).toBeNull()
    expect(meaningNote(source(10, 0))).toBeNull()
  })
  it('shows progress while passages are embedded', () => {
    expect(meaningNote(source(10, 4))).toBe('Searchable by meaning: 4 of 10 passages so far.')
  })
  it('says when every passage is embedded', () => {
    expect(meaningNote(source(10, 10))).toBe('Searchable by meaning, not only by matching words.')
  })
})
