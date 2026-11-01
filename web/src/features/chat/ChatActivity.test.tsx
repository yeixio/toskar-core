import { act, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { stillMascot } from '@/test/stillMascot'
import { ChatActivity } from './ChatActivity'
import { formatElapsed } from '@/lib/elapsed'

describe('ChatActivity', () => {
  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('announces that a reply is in progress', () => {
    render(<ChatActivity label="Searching the web…" />)
    expect(screen.getByRole('status', { name: 'Searching the web…' })).toBeInTheDocument()
    expect(screen.getByText('Searching the web…')).toBeInTheDocument()
  })

  it('shows the time on one step after 15 seconds, and counts again on the next step (#705)', () => {
    stillMascot()
    vi.useFakeTimers()
    const { rerender } = render(<ChatActivity label="Making the picture…" />)
    act(() => vi.advanceTimersByTime(14_000))
    expect(screen.queryByTestId('activity-elapsed')).toBeNull()
    act(() => vi.advanceTimersByTime(66_000))
    expect(screen.getByTestId('activity-elapsed')).toHaveTextContent('1:20')
    // Screen readers hear the step, not the clock.
    expect(screen.getByTestId('activity-elapsed')).toHaveAttribute('aria-hidden', 'true')
    expect(screen.getByRole('status', { name: 'Making the picture…' })).toBeInTheDocument()

    rerender(<ChatActivity label="Writing the answer…" />)
    expect(screen.queryByTestId('activity-elapsed')).toBeNull()
  })
})

describe('formatElapsed', () => {
  it('shows minutes and seconds, and hours past an hour', () => {
    expect(formatElapsed(0)).toBe('0:00')
    expect(formatElapsed(80_000)).toBe('1:20')
    expect(formatElapsed(3_725_000)).toBe('1:02:05')
  })
})
