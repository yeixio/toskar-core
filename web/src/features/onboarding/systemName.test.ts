import { describe, expect, it } from 'vitest'
import i18n from '@/i18n'
import type { Recommendation } from '@/types/api'
import { fitsLine, systemName } from './systemName'

// Regression: toskar-core#1346
describe('onboarding wording', () => {
  const t = i18n.getFixedT('en', 'onboarding')
  it('names the system as people know it', () => {
    expect(systemName('darwin', 'arm64', t)).toBe('macOS, Apple silicon')
    expect(systemName('windows', 'amd64', t)).toBe('Windows')
    expect(systemName('plan9', 'mips', t)).toBe('plan9 / mips')
  })
  it('says the setup fits in the memory the card shows', () => {
    const rec = { reason: 'Recommended X because…', roles: [{ role: 'assistant', model_id: 'q' }], models: [{ id: 'q', display_name: 'Qwen 3.5 9B' }] } as unknown as Recommendation
    expect(fitsLine(rec, 24 * 1024 ** 3, t)).toBe("Qwen 3.5 9B fits in this computer's 24 GB of memory.")
    expect(fitsLine(rec, undefined, t)).toBe('Recommended X because…')
  })
})
