import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { applyLanguage } from '@/i18n'
import { api } from '@/lib/api'
import type { Model, ModelDownloadProgressPayload } from '@/types/api'
import { BrowseAllPanel } from './BrowseAllPanel'

vi.mock('@/lib/api', () => ({
  api: { browseModels: vi.fn(), installModelFromURL: vi.fn() },
}))

const qwen = { id: 'hf-qwen', display_name: 'Qwen3 8B', repo_id: 'Qwen/Qwen3-8B-GGUF', source_url: 'https://hf.co/q.gguf', filename: 'q.gguf' }

function panel(models: Model[], progress: Record<string, ModelDownloadProgressPayload>, failed: Record<string, string> = {}) {
  return <BrowseAllPanel models={models} progress={progress} failed={failed} onClose={vi.fn()} onInstalled={vi.fn()} />
}

describe('BrowseAllPanel', () => {
  beforeEach(async () => {
    await applyLanguage('en')
    vi.mocked(api.browseModels).mockResolvedValue([qwen] as never)
    vi.mocked(api.installModelFromURL).mockReset()
  })

  // The button followed nothing after the request returned, since the
  // download goes on in the background: it looked as if nothing happened.
  it('follows a model from Install to downloading to installed', async () => {
    let finish: (v: { status: string; model_id: string }) => void = () => {}
    vi.mocked(api.installModelFromURL).mockReturnValue(new Promise((resolve) => (finish = resolve)))
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const { rerender } = render(<QueryClientProvider client={client}>{panel([], {})}</QueryClientProvider>)

    fireEvent.click(await screen.findByRole('button', { name: 'Install' }))
    expect(await screen.findByRole('button', { name: 'Starting…' })).toBeDisabled()

    finish({ status: 'downloading', model_id: 'hf-qwen' })
    // Before the model list or any progress has caught up.
    expect(await screen.findByRole('button', { name: 'Downloading…' })).toBeDisabled()

    const progress = { 'hf-qwen': { model_id: 'hf-qwen', percent: 40, bytes_downloaded: 2e9, bytes_total: 5e9 } as ModelDownloadProgressPayload }
    rerender(<QueryClientProvider client={client}>{panel([{ id: 'hf-qwen', installed: false, status: 'downloading' } as Model], progress)}</QueryClientProvider>)
    expect(screen.getByRole('button', { name: 'Downloading…' })).toBeDisabled()
    expect(screen.getByText(/40%/)).toBeInTheDocument()

    rerender(<QueryClientProvider client={client}>{panel([{ id: 'hf-qwen', installed: true, status: 'installed' } as Model], {})}</QueryClientProvider>)
    expect(screen.getByText('Installed')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Install|Downloading/ })).not.toBeInTheDocument()
  })

  it('offers Install again after a download failed', async () => {
    vi.mocked(api.installModelFromURL).mockResolvedValue({ status: 'downloading', model_id: 'hf-qwen' })
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const { rerender } = render(<QueryClientProvider client={client}>{panel([], {})}</QueryClientProvider>)
    fireEvent.click(await screen.findByRole('button', { name: 'Install' }))
    await screen.findByRole('button', { name: 'Downloading…' })
    // The failure event, though the model list may never list it.
    rerender(<QueryClientProvider client={client}>{panel([], {}, { 'hf-qwen': 'Not enough space for the model' })}</QueryClientProvider>)
    expect(screen.getByRole('button', { name: 'Install' })).toBeEnabled()
    expect(screen.getByRole('alert')).toHaveTextContent('Not enough space for the model')
    // Or the model list's own status.
    rerender(<QueryClientProvider client={client}>{panel([{ id: 'hf-qwen', installed: false, status: 'failed' } as Model], {})}</QueryClientProvider>)
    expect(screen.getByRole('button', { name: 'Install' })).toBeEnabled()
  })

  it('shows a model already installed as installed', async () => {
    render(<QueryClientProvider client={new QueryClient()}>{panel([{ id: 'hf-qwen', installed: true } as Model], {})}</QueryClientProvider>)
    expect(await screen.findByText('Installed')).toBeInTheDocument()
  })
})
