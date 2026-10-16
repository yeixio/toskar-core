import { describe, expect, it } from 'vitest'
import { guideUrl } from './guide'

describe('guideUrl', () => {
  it('links a release to its own guide, and anything else to the latest', () => {
    expect(guideUrl('media', '1.9.0')).toBe('https://toskar.ai/docs/1.9.0#media')
    expect(guideUrl('media', '1.9.0-dev+abc')).toBe('https://toskar.ai/docs#media')
    expect(guideUrl('media')).toBe('https://toskar.ai/docs#media')
  })
})
