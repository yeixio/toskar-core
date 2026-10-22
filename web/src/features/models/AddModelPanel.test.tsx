import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { applyLanguage } from '@/i18n'
import { api } from '@/lib/api'
import { AddModelPanel } from './AddModelPanel'

vi.mock('@/lib/api', () => ({
  api: { importModelPath: vi.fn(), findModelsInOtherApps: vi.fn(), installModelFromURL: vi.fn(), previewModelURL: vi.fn(), getModels: vi.fn(), startBenchmark: vi.fn(), getBenchmark: vi.fn(), updateAddedModel: vi.fn(), setModelProjector: vi.fn() },
  uploadModel: vi.fn(),
}))

function renderIt() {
  const onAdded = vi.fn()
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <AddModelPanel onClose={vi.fn()} onAdded={onAdded} />
    </QueryClientProvider>,
  )
  return onAdded
}

describe('AddModelPanel (#467)', () => {
  beforeEach(async () => {
    await applyLanguage('en')
    vi.mocked(api.getModels).mockResolvedValue([{ id: 'qwen', installed: true } as never])
    vi.mocked(api.importModelPath).mockReset().mockResolvedValue({ model_id: 'qwen', status: 'installed', details: { name: 'Qwen3 8B' } })
    vi.mocked(api.findModelsInOtherApps).mockResolvedValue({
      models: [
        { app: 'ollama', path: '/o/blobs/sha256-1', name: 'qwen3-coder:30b', size_bytes: 18e9, parameters: '30B-A3B', quantization: 'Q4_K_M' },
        { app: 'lmstudio', path: '/l/a.gguf', name: 'Already', size_bytes: 1e9, model_id: 'already' },
      ],
    })
  })

  it('adds a file on this computer, used where it is', async () => {
    const onAdded = renderIt()
    fireEvent.click(await screen.findByRole('tab', { name: 'From a file' }))
    fireEvent.change(screen.getByLabelText('Or the location of a file on this computer'), { target: { value: '/m/qwen.gguf' } })
    fireEvent.click(screen.getByLabelText(/Use it where it is/))
    fireEvent.click(screen.getByRole('button', { name: 'Add' }))
    expect(await screen.findByText("Added Qwen3 8B. It's on the Installed tab.")).toBeInTheDocument()
    expect(api.importModelPath).toHaveBeenCalledWith({ path: '/m/qwen.gguf', in_place: true })
    expect(onAdded).toHaveBeenCalledWith('qwen')
  })

  it('lists models other apps downloaded and adds one in place', async () => {
    renderIt()
    // Other apps have models, so that way comes first and is open (#611).
    expect(await screen.findByText('qwen3-coder:30b')).toBeInTheDocument()
    expect(screen.getAllByRole('tab')[0]).toHaveTextContent('From another app')
    expect(screen.getByText(/Ollama · 30B-A3B · Q4_K_M/)).toBeInTheDocument()
    expect(screen.getByText('Added')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Add' }))
    await screen.findByText(/Added qwen3-coder:30b\./)
    expect(api.importModelPath).toHaveBeenCalledWith({ path: '/o/blobs/sha256-1', in_place: true, display_name: 'qwen3-coder:30b' })
  })

  it("names a file put in Toskar's models folder by where it is (#763)", async () => {
    vi.mocked(api.findModelsInOtherApps).mockResolvedValue({
      models: [{ app: 'toskar', path: '/models/mine.gguf', name: 'Gemma 3 1B', size_bytes: 8e8, quantization: 'Q4_K_M' }],
    })
    renderIt()
    expect(await screen.findByText('Gemma 3 1B')).toBeInTheDocument()
    expect(screen.getByText(/Toskar's models folder · Q4_K_M/)).toBeInTheDocument()
  })

  it('checks a model it added: loads it, asks one question, and shows the speed', async () => {
    vi.mocked(api.findModelsInOtherApps).mockResolvedValue({ models: [] })
    vi.mocked(api.startBenchmark).mockResolvedValue({ id: 'b1' } as never)
    vi.mocked(api.getBenchmark).mockResolvedValue({
      id: 'b1', status: 'completed', error: '',
      samples: [{ warmup: true, eval_tok_per_sec: 30 }, { warmup: false, eval_tok_per_sec: 41.6 }],
    } as never)
    renderIt()
    fireEvent.change(await screen.findByLabelText('Or the location of a file on this computer'), { target: { value: '/m/qwen.gguf' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add' }))
    const check = await screen.findByRole('button', { name: 'Check it works' })
    await vi.waitFor(() => expect(check).toBeEnabled())
    fireEvent.click(check)
    expect(await screen.findByText(/42 tokens a second/)).toBeInTheDocument()
    expect(api.startBenchmark).toHaveBeenCalledWith({ model_ids: ['qwen'], workload_ids: ['quick'], runs: 1 })
  })

  it('names and tags what it added, and adds the image support found with it', async () => {
    vi.mocked(api.findModelsInOtherApps).mockResolvedValue({ models: [] })
    vi.mocked(api.importModelPath).mockResolvedValue({ model_id: 'gemma', status: 'installed', details: { name: 'Gemma' }, projector: '/m/mmproj-gemma.gguf' })
    vi.mocked(api.updateAddedModel).mockResolvedValue({ model_id: 'gemma', display_name: 'My Gemma', tags: ['coding'] })
    vi.mocked(api.setModelProjector).mockResolvedValue({ model_id: 'gemma', vision: true })
    renderIt()
    fireEvent.change(await screen.findByLabelText('Or the location of a file on this computer'), { target: { value: '/m/gemma.gguf' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add' }))
    const name = await screen.findByLabelText('Name')
    fireEvent.change(name, { target: { value: 'My Gemma' } })
    fireEvent.click(screen.getByLabelText('Everyday questions'))
    fireEvent.click(screen.getByLabelText('Coding'))
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(await screen.findByText('Saved')).toBeInTheDocument()
    expect(api.updateAddedModel).toHaveBeenCalledWith('gemma', { display_name: 'My Gemma', tags: ['coding'] })
    expect(screen.getByText('mmproj-gemma.gguf')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Add image support' }))
    expect(await screen.findByText(/Image support added/)).toBeInTheDocument()
    expect(api.setModelProjector).toHaveBeenCalledWith('gemma', '/m/mmproj-gemma.gguf')
  })

  it('opens on From a file when no other app has models', async () => {
    vi.mocked(api.findModelsInOtherApps).mockResolvedValue({ models: [] })
    renderIt()
    expect(await screen.findByLabelText('Or the location of a file on this computer')).toBeInTheDocument()
    expect(screen.getAllByRole('tab')[0]).toHaveTextContent('From a file')
  })

  it('says what a link would download before downloading it (#611)', async () => {
    vi.mocked(api.previewModelURL).mockResolvedValue({
      model_id: 'qwen3-5-35b-a3b-q4-k-m',
      filename: 'Qwen3.5-35B-A3B-Q4_K_M.gguf',
      size_bytes: 22e9,
      projector_bytes: 0.9e9,
      free_bytes: 120e9,
    })
    vi.mocked(api.installModelFromURL).mockResolvedValue({ status: 'started', model_id: 'qwen3-5-35b-a3b-q4-k-m' })
    renderIt()
    fireEvent.click(await screen.findByRole('tab', { name: 'From a link' }))
    fireEvent.change(screen.getByLabelText('Link'), { target: { value: 'https://huggingface.co/unsloth/Qwen3.5-35B-A3B-GGUF/resolve/main/Qwen3.5-35B-A3B-Q4_K_M.gguf' } })
    fireEvent.click(screen.getByRole('button', { name: 'Check the link' }))
    expect(await screen.findByText(/A direct download from huggingface\.co: 22\.9 GB with image support, saved on this computer\. Toskar doesn't charge for it\./)).toBeInTheDocument()
    expect(screen.getByText(/120 GB free on this computer\./)).toBeInTheDocument()
    expect(api.installModelFromURL).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'Download 22.9 GB' }))
    await screen.findByText(/Adding .* copying it in/)
    expect(api.installModelFromURL).toHaveBeenCalled()
  })

  it('offers the copy another app has, used where it is, before a second download', async () => {
    vi.mocked(api.previewModelURL).mockResolvedValue({
      model_id: 'qwen',
      filename: 'Qwen3.5-35B-A3B-Q4_K_M.gguf',
      size_bytes: 22e9,
      free_bytes: 10e9,
      elsewhere: [{ app: 'lmstudio', path: '/l/Qwen3.5-35B-A3B-Q4_K_M.gguf', name: 'Qwen3.5 35B', size_bytes: 22e9 }],
    })
    renderIt()
    fireEvent.click(await screen.findByRole('tab', { name: 'From a link' }))
    fireEvent.change(screen.getByLabelText('Link'), { target: { value: 'https://huggingface.co/x/resolve/main/Qwen3.5-35B-A3B-Q4_K_M.gguf' } })
    fireEvent.click(screen.getByRole('button', { name: 'Check the link' }))
    expect(await screen.findByText(/You already have this file in LM Studio/)).toBeInTheDocument()
    // Too big for the space left, so a second copy can't start.
    expect(screen.getByRole('alert')).toHaveTextContent('Not enough space')
    expect(screen.getByRole('button', { name: 'Download a copy anyway' })).toBeDisabled()
    fireEvent.click(screen.getByRole('button', { name: 'Use it where it is' }))
    await screen.findByText(/Added Qwen3\.5 35B\./)
    expect(api.importModelPath).toHaveBeenCalledWith({ path: '/l/Qwen3.5-35B-A3B-Q4_K_M.gguf', in_place: true, display_name: 'Qwen3.5 35B' })
  })
})
