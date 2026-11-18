import { describe, expect, it } from 'vitest'
import computers from '../../../../i18n/locales/en/computers.json'

describe('page names QA regressions', () => {
  // Regression: toskar-core#1244
  it('connecting a phone from Computers does not send people to API Access to manage it', () => {
    const text = JSON.stringify(computers)
    expect(text).not.toMatch(/disconnect it any time in <access>API Access<\/access>/)
  })
})
