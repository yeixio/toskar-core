import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { api } from '@/lib/api'
import type { VoiceChoice, VoicePersonas as Personas } from '@/types/api'
import { VoicePersonas } from './VoicePersonas'

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    api: {
      getVoicePersonas: vi.fn(),
      pickVoicePersona: vi.fn(),
      createVoicePersona: vi.fn(),
      updateVoicePersona: vi.fn(),
      deleteVoicePersona: vi.fn(),
      previewVoice: vi.fn(),
    },
  }
})

const german: VoiceChoice = {
  language: 'de',
  voice: '',
  automatic: 'de_DE-thorsten-medium',
  voices: [
    { id: 'kitten:de:lena', name: 'Lena', engine: 'kitten' },
    { id: 'de_DE-thorsten-medium', name: 'Thorsten', engine: 'piper' },
  ],
}
const greta = { id: 'g', name: 'Greta', language: 'de', voice: 'kitten:de:lena', speed: 1.25, reads: true }

function renderIt(languages?: VoiceChoice[]) {
  return render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <VoicePersonas languages={languages} />
    </QueryClientProvider>,
  )
}

describe('VoicePersonas (#1319)', () => {
  it('lets a Member pick a persona, without making one', async () => {
    vi.mocked(api.getVoicePersonas).mockResolvedValue({ personas: [greta], picked: '' })
    vi.mocked(api.pickVoicePersona).mockResolvedValue({ personas: [greta], picked: 'g' })
    renderIt()
    const pick = await screen.findByRole('combobox', { name: 'Your voice' })
    expect(pick).toHaveDisplayValue("Each language's own voice")
    fireEvent.change(pick, { target: { value: 'g' } })
    await waitFor(() => expect(api.pickVoicePersona).toHaveBeenCalledWith('g'))
    expect(await screen.findByRole('combobox', { name: 'Your voice' })).toHaveDisplayValue('Greta')
    expect(screen.queryByRole('button', { name: 'New persona' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Edit' })).toBeNull()
  })

  it('lets an Admin make, preview, and delete one, and says when its voice is gone', async () => {
    const gone = { ...greta, reads: false, reads_with: 'de_DE-thorsten-medium' }
    vi.mocked(api.getVoicePersonas).mockResolvedValue({ personas: [gone], picked: '' })
    const made: Personas = { personas: [gone, { ...greta, id: 'm', name: 'Max', voice: 'de_DE-thorsten-medium', speed: 1 }], picked: '' }
    vi.mocked(api.createVoicePersona).mockResolvedValue(made)
    vi.mocked(api.deleteVoicePersona).mockResolvedValue({ personas: [], picked: '' })
    vi.mocked(api.previewVoice).mockRejectedValue(new Error('no audio here'))
    renderIt([german])

    expect(await screen.findByText('Lena · German · 1.25×')).toBeInTheDocument()
    expect(screen.getByText("Its voice isn't installed here now, so Thorsten reads instead.")).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'New persona' }))
    fireEvent.change(screen.getByRole('textbox', { name: 'Name' }), { target: { value: 'Max' } })
    expect(screen.getByRole('combobox', { name: 'Voice' })).toHaveDisplayValue('Thorsten (Piper)')
    fireEvent.change(screen.getByRole('slider', { name: 'Speed' }), { target: { value: '1.5' } })
    fireEvent.click(screen.getByRole('button', { name: 'Preview' }))
    await waitFor(() => expect(api.previewVoice).toHaveBeenCalledWith(expect.any(String), 'de_DE-thorsten-medium', 'de', 1.5))
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() =>
      expect(api.createVoicePersona).toHaveBeenCalledWith({ name: 'Max', language: 'de', voice: 'de_DE-thorsten-medium', speed: 1.5 }),
    )
    expect(await screen.findByText('Thorsten · German · 1×')).toBeInTheDocument()

    fireEvent.click(screen.getAllByRole('button', { name: 'Delete' })[0])
    expect(api.deleteVoicePersona).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'Delete Greta' }))
    await waitFor(() => expect(api.deleteVoicePersona).toHaveBeenCalledWith('g'))
  })
})
