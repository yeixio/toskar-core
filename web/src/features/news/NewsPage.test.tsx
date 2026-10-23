import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { applyLanguage } from '@/i18n'
import { api } from '@/lib/api'
import { NewsPage } from './NewsPage'

vi.mock('@/lib/api', () => ({
  api: { getNews: vi.fn(), getModels: vi.fn(), getModelsFit: vi.fn(), installModel: vi.fn(), updateSettings: vi.fn(), getMe: vi.fn() },
}))

const item = (id: string, over: object = {}) => ({
  id,
  date: '2026-10-22',
  kind: 'model' as const,
  title: `Title ${id}`,
  summary: `Summary ${id}.`,
  source: { name: 'Hugging Face', url: `https://huggingface.co/${id}` },
  ...over,
})

function renderIt(role: 'admin' | 'member' = 'admin') {
  vi.mocked(api.getMe).mockResolvedValue({ person: { role } } as never)
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <MemoryRouter>
        <NewsPage />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('NewsPage (#750)', () => {
  beforeEach(async () => {
    await applyLanguage('en')
    vi.mocked(api.getModels).mockResolvedValue([
      { id: 'big-q4', installed: false },
      { id: 'small-q4', installed: true },
    ] as never)
    vi.mocked(api.getModelsFit).mockResolvedValue([{ memory_bytes: 0, winners: [], fits: [{ model_id: 'big-q4', label: 'good', expected_memory_bytes: 0 }] }] as never)
    vi.mocked(api.installModel).mockReset().mockResolvedValue({ status: 'started', model_id: 'big-q4' })
  })

  it('lists the news by day, with Install and fit for a catalog model', async () => {
    vi.mocked(api.getNews).mockResolvedValue({
      enabled: true,
      updated: '2026-10-22T13:20:00Z',
      items: [
        item('a/big', { model_id: 'big-q4' }),
        item('a/small', { model_id: 'small-q4' }),
        item('runtime:llama.cpp/b1', { kind: 'runtime', date: '2026-10-21' }),
      ],
    })
    renderIt()
    expect(await screen.findByText('Title a/big')).toHaveAttribute('href', 'https://huggingface.co/a/big')
    expect(screen.getByText('Summary a/big.')).toBeInTheDocument()
    expect(await screen.findByText('Installed')).toBeInTheDocument()
    expect(await screen.findByText('Good fit')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Install' }))
    expect(await screen.findByText(/Installing/)).toBeInTheDocument()
    expect(api.installModel).toHaveBeenCalledWith('big-q4')
    expect(screen.getByText('Runtime')).toBeInTheDocument()
    expect(screen.getAllByRole('heading', { level: 2 })).toHaveLength(2)
  })

  it('offers no Install to a Member', async () => {
    vi.mocked(api.getNews).mockResolvedValue({ enabled: true, items: [item('a/big', { model_id: 'big-q4' })] })
    renderIt('member')
    expect(await screen.findByText("In Toskar's catalog")).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Install' })).not.toBeInTheDocument()
  })

  it('says when the news is off, and an Admin can turn it on', async () => {
    vi.mocked(api.getNews).mockResolvedValue({ enabled: false, items: [] })
    vi.mocked(api.updateSettings).mockResolvedValue({} as never)
    renderIt()
    fireEvent.click(await screen.findByRole('button', { name: 'Turn on AI news' }))
    await waitFor(() => expect(api.updateSettings).toHaveBeenCalledWith({ ai_news: true }))
  })

  it('shows the kept copy when toskar.ai is out of reach', async () => {
    vi.mocked(api.getNews).mockResolvedValue({ enabled: true, stale: true, fetched_at: '2026-10-21T08:00:00Z', items: [item('a/b')] })
    renderIt()
    expect(await screen.findByText(/couldn't be reached/)).toBeInTheDocument()
  })
})
