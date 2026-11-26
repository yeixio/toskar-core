import { render } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { VoiceAvatar } from './VoiceAvatar'
import { AVATARS, isAvatar, mouthFollower } from './avatars'

describe('VoiceAvatar (#1369)', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('draws each character, hidden from assistive technology, in the mood of the conversation', () => {
    for (const id of AVATARS) {
      const { container, rerender, unmount } = render(<VoiceAvatar character={id} status="listening" />)
      const svg = container.querySelector(`svg[data-avatar="${id}"]`)
      expect(svg).toHaveAttribute('aria-hidden', 'true')
      expect(svg).toHaveAttribute('data-avatar-mood', 'idle')
      rerender(<VoiceAvatar character={id} status="thinking" />)
      expect(container.querySelector('svg')).toHaveAttribute('data-avatar-mood', 'thinking')
      rerender(<VoiceAvatar character={id} status="speaking" />)
      expect(container.querySelector('svg')).toHaveAttribute('data-avatar-mood', 'speaking')
      unmount()
    }
  })

  it('stays still with Reduce Motion', () => {
    vi.stubGlobal('matchMedia', (q: string) => ({ matches: q.includes('reduce'), addEventListener() {}, removeEventListener() {} }))
    const frame = vi.fn()
    vi.stubGlobal('requestAnimationFrame', frame)
    render(<VoiceAvatar character="pebble" status="speaking" level={() => 1} />)
    expect(frame).not.toHaveBeenCalled()
  })

  it('knows only the built-in characters', () => {
    expect(isAvatar('lumen')).toBe(true)
    expect(isAvatar('')).toBe(false)
    expect(isAvatar('someone-famous')).toBe(false)
  })
})

describe('mouthFollower', () => {
  it('opens with the voice, closes in the quiet between words', () => {
    let level = 0
    const mouth = mouthFollower(() => level)
    expect(mouth()).toBe(0)
    level = 0.8
    const opening = mouth()
    expect(opening).toBeGreaterThan(0.4)
    expect(mouth()).toBeGreaterThan(opening)
    level = 0
    for (let i = 0; i < 20; i++) mouth()
    expect(mouth()).toBe(0)
  })
})
