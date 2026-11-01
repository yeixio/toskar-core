import { act, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { stillMascot } from '@/test/stillMascot'
import { MediaProgress } from './MediaProgress'
import { mediaKindOf, mediaProgressFrom } from './mediaProgressState'

describe('MediaProgress (#706)', () => {
  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('says the step and time left, shows how far it has got, and only later what Ratatoskr is up to', () => {
    stillMascot()
    vi.useFakeTimers()
    render(
      <MediaProgress kind="image" progress={{ stage: 'drawing', step: 2, steps: 4, secondsLeft: 3 }} label="Making the picture…" />,
    )
    expect(screen.getByRole('status', { name: 'Making the picture… · Step 2 of 4 · about 3 s left' })).toBeInTheDocument()
    expect(screen.getByRole('img', { name: 'Your picture will appear here' })).toBeInTheDocument()
    expect(screen.getByTestId('media-progress-bar')).toHaveStyle({ width: '50%' })
    expect(screen.queryByTestId('media-progress-fun')).toBeNull()

    act(() => vi.advanceTimersByTime(9_000))
    const fun = screen.getByTestId('media-progress-fun')
    expect(fun).toHaveTextContent(/Ratatoskr/)
    expect(fun).toHaveAttribute('aria-hidden', 'true')
  })

  it('says minutes for a long clip, and a clip has a wide frame', () => {
    render(<MediaProgress kind="video" progress={{ stage: 'drawing', step: 3, steps: 30, secondsLeft: 600 }} label="Making the clip…" />)
    expect(screen.getByRole('status', { name: /about 10 min left/ })).toBeInTheDocument()
    expect(screen.getByRole('img', { name: 'Your clip will appear here' }).className).toContain('aspect-video')
  })

  it('reads core\'s tool.progress', () => {
    expect(mediaKindOf('image.generate')).toBe('image')
    expect(mediaKindOf('internet.search')).toBeNull()
    expect(mediaProgressFrom({ stage: 'drawing', step: 1, steps: 4, seconds_left: 33 })).toEqual({ stage: 'drawing', step: 1, steps: 4, secondsLeft: 33 })
    expect(mediaProgressFrom({ stage: 'other' })).toBeNull()
  })
})
