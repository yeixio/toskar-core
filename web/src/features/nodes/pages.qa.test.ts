import { describe, expect } from 'vitest'
import computers from '../../../../i18n/locales/en/computers.json'
import { knownBug } from '@/test/knownBug'

// QA regressions for where things live. knownBug passes while the bug is
// there and fails once it's fixed (src/test/knownBug.ts).
describe('page names QA regressions', () => {
  // Regression: toskar-core#1244
  knownBug('toskar-core#1244', 'connecting a phone from Computers does not send people to API Access to manage it', () => {
    const text = JSON.stringify(computers)
    expect(text).not.toMatch(/disconnect it any time in <access>API Access<\/access>/)
  })
})
