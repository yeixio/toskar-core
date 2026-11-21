import { describe, expect, it } from 'vitest'
import { IDEAS, buildIdea, ideaDefaults } from './ideas'
import { readNumber } from './number'

describe('automation template QA regressions', () => {
  // Regression: toskar-core#1269
  it('Page watch runs when the page changes, as its summary promises', () => {
    const page = IDEAS.find((idea) => idea.id === 'page')!
    const made = buildIdea(page, { ...ideaDefaults('page'), url: 'example.com/news' }, 'America/Juneau', readNumber)
    expect(made.trigger).toMatchObject({ kind: 'page' })
  })
})
