import { describe, expect } from 'vitest'
import { knownBug } from '@/test/knownBug'
import { IDEAS, buildIdea, ideaDefaults } from './ideas'
import { readNumber } from './number'

// QA regressions for automation templates. knownBug passes while the bug is
// there and fails once it's fixed (src/test/knownBug.ts).
describe('automation template QA regressions', () => {
  // Regression: toskar-core#1269
  knownBug('toskar-core#1269', 'Page watch runs when the page changes, as its summary promises', () => {
    const page = IDEAS.find((idea) => idea.id === 'page')!
    const made = buildIdea(page, { ...ideaDefaults('page'), url: 'example.com/news' }, 'America/Juneau', readNumber)
    expect(made.trigger).toMatchObject({ kind: 'page' })
  })
})
