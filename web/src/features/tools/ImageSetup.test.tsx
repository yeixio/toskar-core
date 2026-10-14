import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { api } from '@/lib/api'
import type { ImageModel, ImageSetup } from '@/types/api'
import { ImageSetupCard } from './ImageSetup'

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return { ...actual, api: { ...actual.api, getMediaSetup: vi.fn(), startMediaSetup: vi.fn(), useMediaBuild: vi.fn(), cancelMediaSetup: vi.fn(), removeMediaModel: vi.fn(), updateSettings: vi.fn() } }
})

const model = (over: Partial<ImageModel>): ImageModel => ({
  id: 'flux2-klein-4b', name: 'FLUX.2 [klein] 4B', description: 'Makes and edits images.', license: 'Apache-2.0',
  memory_bytes: 12 * 1024 ** 3, size_bytes: 5_207_178_964, edits: true, kind: 'image', installed: false, recommended: true, ...over,
})

function renderWith(status: ImageSetup) {
  vi.mocked(api.getMediaSetup).mockResolvedValue(status)
  return render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <ImageSetupCard />
    </QueryClientProvider>,
  )
}

// The Tools page says when a model won't fit or will be slow here, and
// doesn't set up one that won't fit.
describe('ImageSetupCard', () => {
  it("won't set up a model this computer can't hold, and says why", async () => {
    renderWith({ supported: true, ready: false, program: false, release: 'r', memory_bytes: 4 * 1024 ** 3, accelerated: false, models: [model({ too_little_memory: true })] })
    expect(await screen.findByText(/Needs at least .* of memory; this computer has .*, so it can't run here\./)).toBeInTheDocument()
    expect(screen.getByText(/no GPU acceleration for this, so each picture takes a few minutes/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Set up/ })).not.toBeInTheDocument()
  })

  it('warns when memory is tight, and still sets it up', async () => {
    renderWith({ supported: true, ready: false, program: false, release: 'r', memory_bytes: 8 * 1024 ** 3, accelerated: true, models: [model({ tight_memory: true, verdict: 'tight', short: 'memory', need_bytes: 12e9, have_bytes: 8e9 })] })
    expect(await screen.findByText(/Needs about 12 GB of memory to run well; this computer has 8 GB/)).toBeInTheDocument()
    expect(screen.queryByText(/no GPU acceleration/)).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Set up/ })).toBeInTheDocument()
  })

  it('says which build makes them, and offers the GPU build where one runs', async () => {
    vi.mocked(api.useMediaBuild).mockResolvedValue(null)
    renderWith({ supported: true, ready: true, program: true, release: 'r', active: 'flux2-klein-4b', accelerated: false, build: 'cpu', gpu_build: 'vulkan', models: [model({ installed: true })] })
    expect(await screen.findByText('Makes them on the CPU.')).toBeInTheDocument()
    expect(screen.queryByText(/no GPU acceleration/)).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Use the GPU build' }))
    await waitFor(() => expect(api.useMediaBuild).toHaveBeenCalledWith('images', 'gpu'))
  })

  it('offers the CPU build from the Vulkan build', async () => {
    renderWith({ supported: true, ready: true, program: true, release: 'r', active: 'flux2-klein-4b', accelerated: true, build: 'vulkan', gpu_build: 'vulkan', models: [model({ installed: true })] })
    expect(await screen.findByText('Makes them on the GPU (Vulkan).')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Use the CPU build' })).toBeInTheDocument()
  })

  it('sets up a model behind a license only with a token and the person\'s word (#551)', async () => {
    const licensed = model({
      id: 'flux2-klein-9b', name: 'FLUX.2 [klein] 9B', recommended: false, needs_license: true,
      license_url: 'https://huggingface.co/black-forest-labs/FLUX.2-klein-9B', license_allows: 'Non-commercial use.',
    })
    const base = { supported: true, ready: true, program: true, release: 'r', active: 'flux2-klein-4b', accelerated: true, models: [model({ installed: true }), licensed] }
    vi.mocked(api.updateSettings).mockResolvedValue({} as Awaited<ReturnType<typeof api.updateSettings>>)
    vi.mocked(api.startMediaSetup).mockResolvedValue(null)
    renderWith({ ...base, huggingface_token_set: false })
    fireEvent.click(await screen.findByRole('radio', { name: 'FLUX.2 [klein] 9B' }))
    expect(screen.getByText('Needs your license')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Read the license' })).toHaveAttribute('href', licensed.license_url)
    const setUp = screen.getByRole('button', { name: /Download/ })
    expect(setUp).toBeDisabled()

    fireEvent.change(screen.getByLabelText(/Your Hugging Face token/), { target: { value: 'hf_abcdefghijklmnopqrstuvwxyz' } })
    vi.mocked(api.getMediaSetup).mockResolvedValue({ ...base, huggingface_token_set: true })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(api.updateSettings).toHaveBeenCalledWith({ huggingface_token: 'hf_abcdefghijklmnopqrstuvwxyz' }))
    expect(await screen.findByText('Your Hugging Face token is set.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Download/ })).toBeDisabled()

    fireEvent.click(screen.getByRole('checkbox', { name: /My license covers how I'll use this model/ }))
    fireEvent.click(screen.getByRole('button', { name: /Download/ }))
    await waitFor(() => expect(api.startMediaSetup).toHaveBeenCalledWith('images', 'flux2-klein-9b', undefined, { acceptLicense: true, acceptTight: undefined }))
  })

  it('sets up a model that would be slow here only once the person agrees (#550)', async () => {
    vi.mocked(api.startMediaSetup).mockResolvedValue(null)
    renderWith({
      supported: true, ready: true, program: true, release: 'r', active: 'flux2-klein-4b', accelerated: true,
      models: [model({ installed: true }), model({ id: 'big', name: 'Big', recommended: false, graphics_short: true, graphics_bytes: 16 * 1024 ** 3, verdict: 'tight', short: 'graphics', need_bytes: 16e9, have_bytes: 8e9 })],
    })
    fireEvent.click(await screen.findByRole('radio', { name: 'Big' }))
    expect(screen.getByText(/Needs about 16 GB of graphics memory; this computer's graphics card has 8 GB/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Download/ })).toBeDisabled()
    fireEvent.click(screen.getByRole('checkbox', { name: /Set it up anyway/ }))
    fireEvent.click(screen.getByRole('button', { name: /Download/ }))
    await waitFor(() => expect(api.startMediaSetup).toHaveBeenCalledWith('images', 'big', undefined, { acceptLicense: undefined, acceptTight: true }))
  })

  it('names a paired computer a tight model works well on, and sets it up there (#550)', async () => {
    vi.mocked(api.startMediaSetup).mockResolvedValue(null)
    renderWith({
      supported: true, ready: true, program: true, release: 'r', active: 'flux2-klein-4b', accelerated: true,
      models: [model({ installed: true }), model({ id: 'big', name: 'Big', recommended: false, graphics_short: true, graphics_bytes: 16 * 1024 ** 3, verdict: 'tight', short: 'graphics', need_bytes: 16e9, have_bytes: 8e9 })],
      elsewhere: [{ node_id: 'ws', node_name: 'Workstation', model_id: 'big' }],
    })
    expect(await screen.findByText('Works well on Workstation.')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Set it up there' }))
    await waitFor(() => expect(api.startMediaSetup).toHaveBeenCalledWith('images', 'big', 'ws'))
    expect(await screen.findByText(/Setting it up on Workstation/)).toBeInTheDocument()
  })
})
