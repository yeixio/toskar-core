import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { api } from '@/lib/api'
import type { SpecializedAIView } from '@/types/api'
import { exportRevision } from '../display'
import { ExportCard } from './ExportCard'

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return { ...actual, api: { ...actual.api, exportStatus: vi.fn(), startExport: vi.fn(), deleteExport: vi.fn() } }
})

const view = (deployed: number, revisions: number[]) =>
  ({ id: 'ai-1', name: 'Tire Bot', deployed_revision: deployed, revisions: revisions.map((revision) => ({ revision })) }) as unknown as SpecializedAIView

const renderCard = (v: SpecializedAIView) =>
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <ExportCard view={v} />
    </QueryClientProvider>,
  )

describe('ExportCard', () => {
  it('exports the deployed revision, or else the newest', () => {
    expect(exportRevision(view(2, [1, 2, 3]))).toBe(2)
    expect(exportRevision(view(0, [1, 3, 2]))).toBe(3)
    expect(exportRevision(view(0, []))).toBe(0)
  })

  it('starts an export, then offers the file', async () => {
    vi.mocked(api.exportStatus)
      .mockResolvedValueOnce({ ai_id: 'ai-1', revision: 1, state: 'none' })
      .mockResolvedValue({
        ai_id: 'ai-1', revision: 1, state: 'ready', filename: 'tire-bot-r1.gguf', size_bytes: 926118016, instructions: 'You are Tire Bot.',
      })
    vi.mocked(api.startExport).mockResolvedValue({ ai_id: 'ai-1', revision: 1, state: 'exporting' })
    renderCard(view(1, [1]))
    fireEvent.click(await screen.findByRole('button', { name: 'Export' }))
    await waitFor(() => expect(api.startExport).toHaveBeenCalledWith('ai-1', 1))
    const link = await screen.findByRole('link', { name: 'Download tire-bot-r1.gguf' })
    expect(link.getAttribute('href')).toContain('/api/v1/training/ais/ai-1/revisions/1/export/file')
    expect(screen.getByText('883.2 MB')).toBeInTheDocument()
    expect(screen.getByText('You are Tire Bot.')).toBeInTheDocument()
  })

  it('saves through the desktop shell, which streams the file to disk', async () => {
    const SaveDownload = vi.fn().mockResolvedValue('/Users/me/Downloads/tire-bot-r1.gguf')
    const w = window as unknown as { go?: unknown }
    w.go = { main: { App: { SaveDownload } } }
    try {
      vi.mocked(api.exportStatus).mockResolvedValue({
        ai_id: 'ai-1', revision: 1, state: 'ready', filename: 'tire-bot-r1.gguf', size_bytes: 926118016, instructions: '',
      })
      renderCard(view(1, [1]))
      fireEvent.click(await screen.findByRole('link', { name: 'Download tire-bot-r1.gguf' }))
      expect(await screen.findByText('Saved to /Users/me/Downloads/tire-bot-r1.gguf')).toBeInTheDocument()
      expect(SaveDownload).toHaveBeenCalledWith('tire-bot-r1.gguf', '/api/v1/training/ais/ai-1/revisions/1/export/file')
    } finally {
      delete w.go
    }
  })

  it('shows why an export failed', async () => {
    vi.mocked(api.exportStatus).mockResolvedValue({ ai_id: 'ai-1', revision: 1, state: 'failed', error: 'adapter does not match the model' })
    renderCard(view(1, [1]))
    expect(await screen.findByText('The export failed: adapter does not match the model')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Try again' })).toBeInTheDocument()
  })
})
