import { describe, expect, it } from 'vitest'
import type { Model } from '@/types/api'
import { bestForLabel, canChat } from './modelPresentation'

const base: Model = {
  id: 'nomic-embed-text-v1.5-q4',
  display_name: 'Nomic Embed Text',
  capabilities: { tool_calling: false, vision: false, coding: false },
  installed: true,
}

describe('supporting models', () => {
  it('cannot chat and say what they are for', () => {
    expect(canChat({ ...base, support_role: 'embedding' })).toBe(false)
    expect(bestForLabel({ ...base, support_role: 'embedding' })).toBe('Embedding model: helps search knowledge, does not chat')
    expect(bestForLabel({ ...base, support_role: 'reranker' })).toMatch(/^Reranker/)
  })

  it('leaves chat models alone', () => {
    expect(canChat(base)).toBe(true)
  })
})
