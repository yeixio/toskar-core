import { describe, expect, it } from 'vitest'
import { thinkingToSend } from './thinking'

describe('thinkingToSend', () => {
  it('sends only a change from the model’s own way (#593)', () => {
    expect(thinkingToSend(true, false, 'on')).toBeUndefined()
    expect(thinkingToSend(true, false, 'off')).toBe('off')
    expect(thinkingToSend(true, true, 'off')).toBeUndefined()
    expect(thinkingToSend(true, true, 'on')).toBe('on')
    expect(thinkingToSend(false, false, 'off')).toBeUndefined()
    expect(thinkingToSend(false, true, 'on')).toBeUndefined()
  })
})
