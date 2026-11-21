import { describe, expect, it } from 'vitest'
import { formatSize } from './format'

describe('formatSize QA regressions', () => {
  // Regression: toskar-core#1254
  it('small sizes read "4 B", not "4 byte"', () => {
    expect(formatSize(4)).not.toMatch(/\bbyte\b/)
    expect(formatSize(0)).not.toMatch(/\bbyte\b/)
  })
})
