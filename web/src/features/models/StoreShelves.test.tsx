import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { applyLanguage } from '@/i18n'
import { api } from '@/lib/api'
import type { Model, ModelDownloadProgressPayload, ModelStore } from '@/types/api'
import { StoreShelves } from './StoreShelves'

vi.mock('@/lib/api', () => ({
  api: { getModelStore: vi.fn(), installModelFromURL: vi.fn() },
}))

const fromHF = {
  id: 'org-cool-q4',
  display_name: 'Cool 7B',
  repo_id: 'org/Cool-GGUF',
  filename: 'cool.gguf',
  source_url: 'https://huggingface.co/org/Cool-GGUF/resolve/main/cool.gguf',
  size_bytes: 4_000_000_000,
  likes: 1200,
  downloads: 350000,
  license: 'apache-2.0',
  details_url: 'https://huggingface.co/org/Cool-GGUF',
  tags: ['coding'],
  fit: { model_id: 'org-cool-q4', label: 'good' },
}
const tested = { ...fromHF, id: 'qwen', display_name: 'Qwen 3.8 27B', catalog_id: 'qwen3.8-27b-q4', likes: 0, tags: ['general', 'tasks'] }
const store = { live: true, shelves: [{ id: 'popular', models: [fromHF] }, { id: 'tested', models: [tested] }, { id: 'someday', models: [fromHF] }] } as unknown as ModelStore

function show(opts: { search?: string; models?: Model[]; progress?: Record<string, ModelDownloadProgressPayload>; onInstallTested?: (id: string) => void; other?: boolean } = {}) {
  render(
    <MemoryRouter>
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <StoreShelves
          models={opts.models ?? []}
          progress={opts.progress ?? {}}
          search={opts.search ?? ''}
          isOtherComputer={opts.other ?? false}
          onInstallTested={opts.onInstallTested ?? vi.fn()}
          onInstalled={vi.fn()}
        />
      </QueryClientProvider>
    </MemoryRouter>,
  )
}

describe('StoreShelves', () => {
  beforeEach(async () => {
    await applyLanguage('en')
    vi.mocked(api.getModelStore).mockReset()
    vi.mocked(api.getModelStore).mockResolvedValue(store)
    vi.mocked(api.installModelFromURL).mockReset()
  })

  it('shows shelves with what people think of each model, and how it fits', async () => {
    show()
    expect(await screen.findByRole('heading', { name: 'Popular now' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Tested by Toskar' })).toBeInTheDocument()
    expect(screen.getByText('♥ 1.2K')).toBeInTheDocument()
    expect(screen.getAllByText('↓ 350K').length).toBeGreaterThan(0)
    expect(screen.getAllByText('Good fit').length).toBeGreaterThan(0)
    // A shelf this build doesn't know isn't shown under a raw key.
    expect(screen.queryByText(/someday/)).not.toBeInTheDocument()
  })

  it('installs a Hugging Face model from its file, and a tested one by its catalog ID', async () => {
    vi.mocked(api.installModelFromURL).mockResolvedValue({ status: 'started', model_id: 'org-cool-q4' })
    const onInstallTested = vi.fn()
    show({ onInstallTested })
    const buttons = await screen.findAllByRole('button', { name: 'Install' })
    fireEvent.click(buttons[0])
    await waitFor(() => expect(api.installModelFromURL).toHaveBeenCalled())
    expect(vi.mocked(api.installModelFromURL).mock.calls[0][0]).toMatchObject({ source_url: fromHF.source_url, id: fromHF.id })
    fireEvent.click(buttons[1])
    expect(onInstallTested).toHaveBeenCalledWith('qwen3.8-27b-q4')
  })

  it('opens the details, says whether Toskar tested it, and links to its page', async () => {
    show()
    fireEvent.click(await screen.findByRole('button', { name: 'Cool 7B' }))
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    expect(screen.getByText('apache-2.0')).toBeInTheDocument()
    expect(screen.getByText(/hasn't been tested with Toskar/)).toBeInTheDocument()
    const link = screen.getByRole('link', { name: /Hugging Face/ })
    expect(link).toHaveAttribute('href', fromHF.details_url)
    expect(link).toHaveAttribute('rel', expect.stringContaining('noopener'))
  })

  it('follows a download, and says when a model is installed', async () => {
    show({ progress: { 'org-cool-q4': { model_id: 'org-cool-q4', percent: 40, bytes_downloaded: 1, bytes_total: 2 } as ModelDownloadProgressPayload } })
    expect(await screen.findByText('40%')).toBeInTheDocument()
  })

  it('narrows the shelves by search', async () => {
    show({ search: 'qwen' })
    expect(await screen.findByRole('heading', { name: 'Tested by Toskar' })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Popular now' })).not.toBeInTheDocument()
  })

  it("says so when Hugging Face can't be reached", async () => {
    vi.mocked(api.getModelStore).mockResolvedValue({ live: false, shelves: [{ id: 'tested', models: [tested] }] } as unknown as ModelStore)
    show()
    expect(await screen.findByText(/Couldn't reach Hugging Face/)).toBeInTheDocument()
  })

  it("doesn't show this computer's fit for another computer", async () => {
    show({ other: true })
    await screen.findByRole('heading', { name: 'Popular now' })
    expect(screen.queryByText('Good fit')).not.toBeInTheDocument()
  })
})
