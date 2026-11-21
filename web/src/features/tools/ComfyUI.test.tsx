import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/lib/api'
import { ComfyUICard } from './ComfyUI'

vi.mock('@/lib/api', () => ({
  api: {
    getMe: vi.fn(),
    getComfyUI: vi.fn(),
    setComfyUI: vi.fn(),
    listComfyWorkflows: vi.fn(),
    detectComfyWorkflow: vi.fn(),
    saveComfyWorkflow: vi.fn(),
    deleteComfyWorkflow: vi.fn(),
  },
}))

const graph = { '6': { class_type: 'CLIPTextEncode', inputs: { text: 'a cat' } } }

function renderIt() {
  return render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <ComfyUICard />
    </QueryClientProvider>,
  )
}

function admin(role: 'admin' | 'member') {
  vi.mocked(api.getMe).mockResolvedValue({ person: { role } } as Awaited<ReturnType<typeof api.getMe>>)
}

describe('ComfyUICard', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    admin('admin')
    vi.mocked(api.getComfyUI).mockResolvedValue({ base_url: '', has_token: false })
    vi.mocked(api.listComfyWorkflows).mockResolvedValue([])
  })

  it('is only for Admins', async () => {
    admin('member')
    renderIt()
    await waitFor(() => expect(api.getMe).toHaveBeenCalled())
    expect(screen.queryByText('ComfyUI')).not.toBeInTheDocument()
    expect(api.getComfyUI).not.toHaveBeenCalled()
  })

  it('connects and shows what the check found', async () => {
    vi.mocked(api.setComfyUI).mockResolvedValue({
      base_url: 'http://rig:8188',
      has_token: true,
      info: { version: '0.3.40', devices: [{ name: 'cuda:0 RTX 4090', type: 'cuda', vram_total: 24e9, vram_free: 20e9 }], models: { checkpoints: ['a', 'b'], loras: ['c'] }, node_types: 812 },
    })
    renderIt()
    fireEvent.change(await screen.findByLabelText('ComfyUI address'), { target: { value: 'http://rig:8188' } })
    fireEvent.change(screen.getByLabelText('Token'), { target: { value: 'secret' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save and check' }))
    await waitFor(() => expect(api.setComfyUI).toHaveBeenCalledWith({ base_url: 'http://rig:8188', token: 'secret' }))
    expect(await screen.findByText('Connected to ComfyUI 0.3.40, with 812 node types.')).toBeInTheDocument()
    expect(screen.getByText('cuda:0 RTX 4090, 24 GB')).toBeInTheDocument()
    expect(screen.getByText('2 models and 1 LoRAs.')).toBeInTheDocument()
    expect(await screen.findByText('No workflows yet. Add one exported from ComfyUI.')).toBeInTheDocument()
  })

  it("says why a check failed", async () => {
    vi.mocked(api.getComfyUI).mockResolvedValue({ base_url: 'http://rig:8188', has_token: false, error: "ComfyUI at rig:8188 didn't answer" })
    renderIt()
    expect(await screen.findByRole('alert')).toHaveTextContent("didn't answer")
  })

  it('adds a workflow from an exported file, showing the inputs it found', async () => {
    vi.mocked(api.getComfyUI).mockResolvedValue({ base_url: 'http://127.0.0.1:8188', has_token: false, info: { devices: [], models: {}, node_types: 10 } })
    vi.mocked(api.detectComfyWorkflow).mockResolvedValue({ prompt: { node: '6', field: 'text' } })
    vi.mocked(api.saveComfyWorkflow).mockResolvedValue({ name: 'watercolor', inputs: { prompt: { node: '6', field: 'text' } }, nodes: 1 })
    renderIt()
    const file = new File([JSON.stringify(graph)], 'watercolor.json', { type: 'application/json' })
    fireEvent.change(await screen.findByLabelText('Workflow file'), { target: { files: [file] } })
    expect(await screen.findByText('Inputs found: prompt (node 6).')).toBeInTheDocument()
    expect(screen.getByLabelText('Name')).toHaveValue('watercolor')
    fireEvent.change(screen.getByLabelText('What it does'), { target: { value: 'Pictures in watercolor' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add workflow' }))
    await waitFor(() =>
      expect(api.saveComfyWorkflow).toHaveBeenCalledWith({ name: 'watercolor', description: 'Pictures in watercolor', graph, inputs: { prompt: { node: '6', field: 'text' } } }),
    )
  })

  it('removes a saved workflow', async () => {
    vi.mocked(api.getComfyUI).mockResolvedValue({ base_url: 'http://127.0.0.1:8188', has_token: false })
    vi.mocked(api.listComfyWorkflows).mockResolvedValue([{ name: 'upscale', description: 'Upscales 4×', inputs: { image: { node: '1', field: 'image' } }, nodes: 4 }])
    vi.mocked(api.deleteComfyWorkflow).mockResolvedValue(null)
    renderIt()
    expect(await screen.findByText('picture (node 1)')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Remove upscale' }))
    await waitFor(() => expect(api.deleteComfyWorkflow).toHaveBeenCalledWith('upscale'))
  })
})
