import { describe, expect, it } from 'vitest'
import { doorstepProfileTemplate } from './profilePresets'

describe('doorstepProfileTemplate', () => {
  // A front door talks with strangers (#667): no tools, no memory, an
  // enforced topic, and the public audience that keeps the owner out.
  it('starts with no tools, no memory, and an enforced topic', () => {
    const p = doorstepProfileTemplate()
    expect(p.audience).toBe('public')
    expect(p.tools).toEqual([])
    expect(p.orchestration?.memory).toBe('off')
    expect(p.topics?.strictness).toBe('enforce')
    expect(p.topics?.stays_on).toMatch(/front door/i)
    expect(p.topics?.never_discuss).toHaveLength(3)
    expect(p.name).toBe('Front door')
  })
})
