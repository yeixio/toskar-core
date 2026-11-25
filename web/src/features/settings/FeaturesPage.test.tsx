import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import { api } from '@/lib/api'
import type { Feature } from '@/types/api'
import { FeaturesPage } from './FeaturesPage'

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return { ...actual, api: { ...actual.api, listFeatures: vi.fn(), setUpFeature: vi.fn() } }
})

const voice: Feature = {
  id: 'voice',
  ready: false,
  installing: false,
  setup_bytes: 3_000_000_000,
  needs: [
    { need: 'voice', ready: false, install: 'voice', name: 'Kitten TTS' },
    { need: 'chat', ready: false, install: 'model', option: 'gemma', name: 'Gemma 4 E4B', size_bytes: 3_000_000_000 },
  ],
}
const door: Feature = {
  id: 'door',
  ready: false,
  installing: false,
  setup_bytes: 0,
  needs: [{ need: 'camera', ready: false }],
}
const pictures: Feature = { id: 'pictures', ready: true, installing: false, setup_bytes: 0, needs: [{ need: 'images', ready: true }] }

function renderPage() {
  return render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <MemoryRouter>
        <FeaturesPage />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

// #1355: each feature says Ready or what it needs, and one press sets up all of it.
describe('FeaturesPage', () => {
  it('lists what each feature needs and sets one up in one step', async () => {
    vi.mocked(api.listFeatures).mockResolvedValue([voice, door, pictures])
    vi.mocked(api.setUpFeature).mockResolvedValue({ ...voice, installing: true })
    renderPage()
    const voiceRow = (await screen.findByRole('heading', { name: 'Voice conversations' })).closest('li')!
    expect(within(voiceRow).getByText('Needs setup')).toBeInTheDocument()
    expect(within(voiceRow).getByText(/A chat model: installs Gemma 4 E4B/)).toBeInTheDocument()
    expect(within(voiceRow).getByText(/A voice to speak and listen: installs Kitten TTS/)).toBeInTheDocument()

    // Nothing here can install a camera, so it says where to connect one.
    const doorRow = screen.getByRole('heading', { name: 'Answer the door' }).closest('li')!
    expect(within(doorRow).getByRole('link', { name: 'Connected services' })).toHaveAttribute('href', '/settings/tools')
    expect(within(doorRow).queryByRole('button')).not.toBeInTheDocument()

    const picturesRow = screen.getByRole('heading', { name: 'Make pictures' }).closest('li')!
    expect(within(picturesRow).getByText('Ready')).toBeInTheDocument()

    fireEvent.click(within(voiceRow).getByRole('button', { name: /^Set up/ }))
    await waitFor(() => expect(api.setUpFeature).toHaveBeenCalledWith('voice'))
  })

  it('shows one progress line while a feature sets up', async () => {
    vi.mocked(api.listFeatures).mockResolvedValue([
      { ...voice, installing: true, needs: [{ ...voice.needs[1], installing: true, percent: 40 }] },
    ])
    renderPage()
    expect(await screen.findByText('Setting up… 40%')).toBeInTheDocument()
    expect(screen.getAllByRole('progressbar')).toHaveLength(1)
    expect(screen.queryByRole('button', { name: /^Set up/ })).not.toBeInTheDocument()
  })
})
