import { afterEach, describe, expect, it } from 'vitest'
import { applyLanguage } from '@/i18n'
import { profileName } from './profilePresentation'

afterEach(async () => {
  await applyLanguage('en')
})

// Built-in profile names come from the server in English (#1271).
describe('profileName', () => {
  it("shows a built-in profile's name in the app's language", async () => {
    await applyLanguage('de')
    expect(profileName({ id: 'general-assistant', name: 'General Assistant' })).toBe('Allgemeiner Assistent')
    expect(profileName({ id: 'programming', name: 'Programming' })).toBe('Programmieren')
  })

  it('keeps a name someone gave, and any other profile as named', async () => {
    await applyLanguage('de')
    expect(profileName({ id: 'general-assistant', name: 'Mein Assistent' })).toBe('Mein Assistent')
    expect(profileName({ id: 'p-1', name: 'General Assistant' })).toBe('General Assistant')
  })
})
