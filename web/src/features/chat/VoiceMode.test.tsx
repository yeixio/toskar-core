import { fireEvent, render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { applyLanguage } from '@/i18n'
import { VoiceMode, VoiceModeButton } from './VoiceMode'

describe('VoiceMode', () => {
  beforeEach(async () => {
    await applyLanguage('en')
  })

  it('says why when the page cannot use a microphone, and ends', () => {
    // jsdom has neither a microphone nor Web Audio, like an http:// page on another device.
    const onClose = vi.fn()
    render(
      <QueryClientProvider client={new QueryClient()}>
        <VoiceMode onClose={onClose} />
      </QueryClientProvider>,
    )
    expect(screen.getByRole('dialog', { name: 'Voice mode' })).toBeTruthy()
    expect(screen.getByText(/can't use a microphone/)).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Mute' })).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'End' }))
    expect(onClose).toHaveBeenCalled()
  })

  it('starts from the composer', () => {
    const onStart = vi.fn()
    render(<VoiceModeButton onStart={onStart} />)
    fireEvent.click(screen.getByRole('button', { name: 'Talk' }))
    expect(onStart).toHaveBeenCalled()
  })
})
