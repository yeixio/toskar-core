import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { api } from '@/lib/api'
import type { VoiceSettings as Settings } from '@/types/api'
import { VoiceSettings } from './VoiceSettings'

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    api: {
      getVoiceSettings: vi.fn(),
      setSpeechVoice: vi.fn(),
      installKitten: vi.fn(),
      removeKitten: vi.fn(),
      previewVoice: vi.fn(),
    },
  }
})

const kitten = { credit: 'Powered by KittenML', url: 'https://huggingface.co/KittenML/kitten-tts-2-mini', license: 'Apache-2.0', languages: ['de', 'en'] }

function settings(state: Settings['kitten']['state'], german: Settings['languages'][number]): Settings {
  return { kitten: { ...kitten, state }, languages: [german] }
}

const piperGerman = {
  language: 'de',
  voice: '',
  automatic: 'de_DE-thorsten-medium',
  voices: [{ id: 'de_DE-thorsten-medium', name: 'Thorsten', engine: 'piper' as const }],
}

function renderIt() {
  return render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <VoiceSettings />
    </QueryClientProvider>,
  )
}

describe('VoiceSettings (#1304)', () => {
  it("installs Kitten, shows its credit, and chooses a language's voice", async () => {
    vi.mocked(api.getVoiceSettings).mockResolvedValue(settings('not_installed', piperGerman))
    const withKitten: Settings['languages'][number] = {
      ...piperGerman,
      automatic: 'kitten:de:lena',
      voices: [{ id: 'kitten:de:lena', name: 'Lena', description: 'Calm (female)', engine: 'kitten' }, ...piperGerman.voices],
    }
    vi.mocked(api.installKitten).mockResolvedValue(settings('installed', withKitten))
    vi.mocked(api.setSpeechVoice).mockResolvedValue(settings('installed', { ...withKitten, voice: 'de_DE-thorsten-medium' }))
    renderIt()

    expect(await screen.findByRole('link', { name: 'Powered by KittenML' })).toHaveAttribute('href', kitten.url)
    fireEvent.click(screen.getByRole('button', { name: 'Install' }))
    await waitFor(() => expect(api.installKitten).toHaveBeenCalled())
    expect(await screen.findByRole('button', { name: 'Remove' })).toBeInTheDocument()

    const german = screen.getByRole('combobox', { name: 'German' })
    expect(german).toHaveDisplayValue('Automatic: Lena (Kitten) · Calm (female)')
    fireEvent.change(german, { target: { value: 'de_DE-thorsten-medium' } })
    await waitFor(() => expect(api.setSpeechVoice).toHaveBeenCalledWith('de', 'de_DE-thorsten-medium'))
  })

  it('asks before removing Kitten', async () => {
    vi.mocked(api.getVoiceSettings).mockResolvedValue(settings('installed', piperGerman))
    vi.mocked(api.removeKitten).mockResolvedValue(settings('not_installed', piperGerman))
    renderIt()
    fireEvent.click(await screen.findByRole('button', { name: 'Remove' }))
    expect(api.removeKitten).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'Remove Kitten' }))
    await waitFor(() => expect(api.removeKitten).toHaveBeenCalled())
    expect(await screen.findByRole('button', { name: 'Install' })).toBeInTheDocument()
  })

  it('previews a voice in its own language', async () => {
    vi.mocked(api.getVoiceSettings).mockResolvedValue(settings('not_installed', piperGerman))
    vi.mocked(api.previewVoice).mockRejectedValue(new Error('offline'))
    renderIt()
    fireEvent.click(await screen.findByRole('button', { name: 'Preview the German voice' }))
    await waitFor(() => expect(api.previewVoice).toHaveBeenCalledWith(expect.stringContaining('Hallo'), 'de_DE-thorsten-medium', 'de'))
    expect(await screen.findByText("Couldn't play the preview.")).toBeInTheDocument()
  })
})
