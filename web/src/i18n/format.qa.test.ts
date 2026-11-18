import { describe, expect } from 'vitest'
import { knownBug } from '@/test/knownBug'
import { formatSize } from './format'

// QA regressions: knownBug passes while the bug is there and fails once
// it's fixed (src/test/knownBug.ts).
describe('formatSize QA regressions', () => {
  // Regression: toskar-core#1254
  knownBug('toskar-core#1254', 'small sizes read "4 B", not "4 byte"', () => {
    expect(formatSize(4)).not.toMatch(/\bbyte\b/)
    expect(formatSize(0)).not.toMatch(/\bbyte\b/)
  })
})
