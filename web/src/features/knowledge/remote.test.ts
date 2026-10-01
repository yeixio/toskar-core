import { describe, expect, it } from 'vitest'
import type { KnowledgeSource } from '@/types/api'
import { refreshNote, sourceBadge, sourceWhere } from './remote'

const base = { id: 'k', name: 'n', status: 'ready', chunk_count: 1, created_at: '', updated_at: '' } as const

describe('remote sources', () => {
  it('labels each kind', () => {
    expect(sourceBadge({ ...base, kind: 'path' })).toBe('Linked')
    expect(sourceBadge({ ...base, kind: 'text' })).toBe('Copy')
    expect(sourceBadge({ ...base, kind: 'database', remote: { driver: 'postgres', refresh_minutes: 60 } })).toBe('PostgreSQL')
    expect(sourceBadge({ ...base, kind: 'api', remote: { url: 'https://x', refresh_minutes: 5 } })).toBe('Web API')
  })
  it('says where the data comes from and when it refreshes', () => {
    const api: KnowledgeSource = { ...base, kind: 'api', remote: { url: 'https://shop.example/stock', refresh_minutes: 120 } }
    expect(sourceWhere(api)).toBe('https://shop.example/stock')
    expect(refreshNote(api)).toContain('more than 2 hours old')
    expect(refreshNote({ ...base, kind: 'database', remote: { query: 'SELECT 1', refresh_minutes: 1 } })).toContain('1 minute old')
    expect(refreshNote({ ...base, kind: 'text' })).toBeNull()
  })
})
