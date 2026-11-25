import { describe, expect, it } from 'vitest'
import type { Model, Recommendation } from '@/types/api'
import { modelOnHand, withModelOnHand } from './onHand'

const model = (id: string, extra: Partial<Model> = {}) => ({ id, display_name: id, installed: false, ...extra }) as Model

const rec: Recommendation = {
  purpose: 'general',
  reason: '',
  estimated_storage_bytes: 4_400_000_000,
  estimated_vram_bytes: 6_000_000_000,
  models: [model('qwen2.5-7b', { size_bytes: 4_400_000_000 }), model('nomic-embed', { support_role: 'embedding' })],
  roles: [
    { role: 'assistant', model_id: 'qwen2.5-7b', required: true },
    { role: 'embedding', model_id: 'nomic-embed', required: false },
  ],
}

// #1214: a model added from a file is offered before a download, so setup works offline.
describe('modelOnHand', () => {
  it('offers the largest chat model already here, and nothing when the setup needs no download', () => {
    const here = [
      model('qwen3.5-4b', { installed: true, size_bytes: 2_700_000_000 }),
      model('gemma-1b', { installed: true, size_bytes: 800_000_000 }),
      model('embed', { installed: true, support_role: 'embedding', size_bytes: 9_000_000_000 }),
    ]
    expect(modelOnHand(rec, here)?.id).toBe('qwen3.5-4b')
    expect(modelOnHand(rec, [model('x')])).toBeNull()
    const ready = { ...rec, models: rec.models.map((m) => ({ ...m, installed: true })) }
    expect(modelOnHand(ready, here)).toBeNull()
  })

  it('puts the model on hand in the chat roles and downloads nothing', () => {
    const onHand = model('qwen3.5-4b', { installed: true })
    const out = withModelOnHand(rec, onHand)
    expect(out.models.map((m) => m.id)).toEqual(['qwen3.5-4b'])
    expect(out.roles).toEqual([{ role: 'assistant', model_id: 'qwen3.5-4b', required: true }])
    expect(out.models.every((m) => m.installed)).toBe(true)
  })
})
