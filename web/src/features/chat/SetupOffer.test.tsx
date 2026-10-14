import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { applyLanguage } from '@/i18n'
import { api } from '@/lib/api'
import type { ImageSetup, SetupOffer } from '@/types/api'
import { SetupOfferCard } from './SetupOffer'

vi.mock('@/lib/api', () => ({
  api: { getMediaSetup: vi.fn(), startMediaSetup: vi.fn(), cancelMediaSetup: vi.fn(), getModels: vi.fn(), installModel: vi.fn() },
}))

// The events the vision card follows, sent by the test.
let sendEvent: ((event: { type: string; payload?: unknown }) => void) | null = null
vi.mock('@/lib/events', () => ({
  subscribeEvents: ({ onEvent }: { onEvent: (event: { type: string; payload?: unknown }) => void }) => {
    sendEvent = onEvent
    return () => {
      sendEvent = null
    }
  },
}))

const offer: SetupOffer = {
  ability: 'image_generation',
  label: 'Generate images',
  option: 'flux2-klein-4b',
  name: 'FLUX.2 [klein] 4B',
  size_bytes: 5_207_178_964,
  node_name: 'Studio',
  request: 'Make me an image of a Viking tree',
}

function status(over: Partial<ImageSetup>): ImageSetup {
  return { supported: true, ready: false, program: false, release: 'r', models: [], ...over }
}

function renderIt(onContinue: (text: string, attachments?: string[]) => void, o: SetupOffer = offer) {
  return render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <SetupOfferCard offer={o} onContinue={onContinue} />
    </QueryClientProvider>,
  )
}

describe('SetupOfferCard', () => {
  beforeEach(async () => {
    await applyLanguage('en')
    vi.mocked(api.getMediaSetup).mockReset()
    vi.mocked(api.startMediaSetup).mockReset()
  })

  it('says what it would install, then finishes the request once it is ready', async () => {
    vi.mocked(api.getMediaSetup).mockResolvedValue(status({}))
    const onContinue = vi.fn()
    renderIt(onContinue)
    expect(await screen.findByText('FLUX.2 [klein] 4B · 5.2 GB download · on Studio (this computer)')).toBeInTheDocument()

    vi.mocked(api.startMediaSetup).mockResolvedValue(
      status({ ready: true, program: true, active: 'flux2-klein-4b', job: { model_id: 'flux2-klein-4b', stage: 'model', done_bytes: 1, total_bytes: 1, running: false } }),
    )
    fireEvent.click(screen.getByRole('button', { name: 'Set up' }))
    await waitFor(() => expect(onContinue).toHaveBeenCalledWith('Make me an image of a Viking tree'))
    expect(api.startMediaSetup).toHaveBeenCalledWith('images', 'flux2-klein-4b', undefined, { acceptTight: false })
    expect(await screen.findByText('Set up. Finishing your request…')).toBeInTheDocument()
    expect(onContinue).toHaveBeenCalledTimes(1)
  })

  it('says before setup when it will be slow here, or may fail', async () => {
    vi.mocked(api.getMediaSetup).mockResolvedValue(status({}))
    renderIt(vi.fn(), { ...offer, slow: true, tight_memory: true })
    expect(await screen.findByText(/no GPU acceleration for it, so each picture takes a few minutes/)).toBeInTheDocument()
    expect(screen.getByText(/less memory than it's comfortable with/)).toBeInTheDocument()
  })

  it('says what a tight model needs and what the computer has (#550)', async () => {
    vi.mocked(api.getMediaSetup).mockResolvedValue(status({}))
    renderIt(vi.fn(), { ...offer, tight_memory: true, verdict: 'tight', short: 'shared', need_bytes: 12e9, have_bytes: 10e9 })
    expect(await screen.findByText(/Needs about 12 GB of graphics memory; this computer's graphics can use 10 GB/)).toBeInTheDocument()
  })

  it('offers to continue when it was set up elsewhere', async () => {
    vi.mocked(api.getMediaSetup).mockResolvedValue(status({ ready: true, program: true, active: 'flux2-klein-4b' }))
    const onContinue = vi.fn()
    renderIt(onContinue)
    fireEvent.click(await screen.findByRole('button', { name: 'Continue' }))
    expect(onContinue).toHaveBeenCalledWith('Make me an image of a Viking tree')
  })

  it('shows the download while it runs', async () => {
    vi.mocked(api.getMediaSetup).mockResolvedValue(
      status({ job: { model_id: 'flux2-klein-4b', stage: 'model', done_bytes: 2_600_000_000, total_bytes: 5_200_000_000, running: true } }),
    )
    renderIt(vi.fn())
    expect(await screen.findByText('Downloading… 2.6 GB of 5.2 GB')).toBeInTheDocument()
    expect(screen.getByRole('progressbar')).toHaveAttribute('aria-valuenow', '50')
  })

  it('sets up video generation for a video request', async () => {
    vi.mocked(api.getMediaSetup).mockResolvedValue(status({}))
    vi.mocked(api.startMediaSetup).mockResolvedValue(status({ ready: true }))
    const onContinue = vi.fn()
    renderIt(onContinue, { ...offer, ability: 'video_generation', option: 'wan2.2-ti2v-5b', name: 'Wan 2.2 TI2V 5B', size_bytes: 8_497_662_272, request: 'Make a video of waves' })
    fireEvent.click(await screen.findByRole('button', { name: 'Set up' }))
    await waitFor(() => expect(onContinue).toHaveBeenCalledWith('Make a video of waves'))
    expect(api.getMediaSetup).toHaveBeenCalledWith('video', undefined)
    expect(api.startMediaSetup).toHaveBeenCalledWith('video', 'wan2.2-ti2v-5b', undefined, { acceptTight: false })
  })

  it('sets up on a paired computer, names it and its free space, and follows the download there', async () => {
    const remote: SetupOffer = { ...offer, node_id: 'ws', node_name: 'Workstation', remote: true, free_bytes: 120_000_000_000, slow: true }
    vi.mocked(api.getMediaSetup).mockResolvedValue(status({}))
    renderIt(vi.fn(), remote)
    expect(await screen.findByText('FLUX.2 [klein] 4B · 5.2 GB download · on Workstation · 120 GB free')).toBeInTheDocument()
    expect(screen.getByText('Workstation has no GPU acceleration for it, so each picture takes a few minutes.')).toBeInTheDocument()
    expect(api.getMediaSetup).toHaveBeenCalledWith('images', 'ws')

    vi.mocked(api.startMediaSetup).mockResolvedValue(
      status({ job: { model_id: 'flux2-klein-4b', stage: 'model', done_bytes: 1_000_000_000, total_bytes: 5_000_000_000, running: true } }),
    )
    fireEvent.click(screen.getByRole('button', { name: 'Set up' }))
    expect(await screen.findByText('Downloading… 1 GB of 5 GB')).toBeInTheDocument()
    expect(api.startMediaSetup).toHaveBeenCalledWith('images', 'flux2-klein-4b', 'ws', { acceptTight: false })
  })

  it('installs a model that can see pictures, then asks again with the pictures (#529)', async () => {
    const vision: SetupOffer = {
      ability: 'vision',
      label: 'Seeing pictures',
      option: 'gemma-3-4b-q4',
      name: 'Gemma 3 4B',
      size_bytes: 4_192_260_064,
      request: 'What type of dog is this?',
      attachments: ['art-1'],
    }
    const model = (installed: boolean) => [{ id: 'gemma-3-4b-q4', installed }] as Awaited<ReturnType<typeof api.getModels>>
    vi.mocked(api.getModels).mockResolvedValue(model(false))
    vi.mocked(api.installModel).mockResolvedValue({ status: 'downloading', model_id: 'gemma-3-4b-q4' })
    const onContinue = vi.fn()
    renderIt(onContinue, vision)
    expect(await screen.findByText('Gemma 3 4B · 4.2 GB download · on this computer')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Set up' }))
    await waitFor(() => expect(sendEvent).not.toBeNull())
    expect(api.installModel).toHaveBeenCalledWith('gemma-3-4b-q4')
    sendEvent!({ type: 'model.download.progress', payload: { model_id: 'gemma-3-4b-q4', percent: 50, bytes_downloaded: 1, bytes_total: 2 } })
    expect(await screen.findByText('Downloading… 2.1 GB of 4.2 GB')).toBeInTheDocument()
    expect(onContinue).not.toHaveBeenCalled()

    vi.mocked(api.getModels).mockResolvedValue(model(true))
    sendEvent!({ type: 'model.download.completed', payload: { model_id: 'gemma-3-4b-q4' } })
    await waitFor(() => expect(onContinue).toHaveBeenCalledWith('What type of dog is this?', ['art-1']))
    expect(onContinue).toHaveBeenCalledTimes(1)
  })
})
