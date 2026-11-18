import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { applyLanguage } from '@/i18n'
import { api } from '@/lib/api'
import { WhatLeft } from './WhatLeft'

vi.mock('@/lib/api', () => ({
  api: { getPrivacy: vi.fn(), listEgress: vi.fn(), setRunRetention: vi.fn(), deleteRunRecords: vi.fn() },
}))

function renderIt() {
  return render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <WhatLeft />
    </QueryClientProvider>,
  )
}

describe('WhatLeft', () => {
  beforeEach(() => {
    vi.mocked(api.getPrivacy).mockResolvedValue({ retention_days: 30, last_30_days: { web_search: 2, connector: 1 } })
    vi.mocked(api.listEgress).mockResolvedValue([
      { id: '1', at: '2026-10-01T20:00:00Z', kind: 'web_search', destination: 'DuckDuckGo', detail: 'tide times', source: 'chat' },
      { id: '2', at: '2026-10-01T20:01:00Z', kind: 'connector', destination: 'GitHub', detail: 'Search GitHub (query: is:open)', source: 'api' },
    ])
    vi.mocked(api.setRunRetention).mockResolvedValue({ retention_days: 7, last_30_days: {} })
    vi.mocked(api.deleteRunRecords).mockResolvedValue({ tasks: 4, automation_runs: 1, egress: 2 })
  })

  it('lists what left, changes retention, and deletes run records', async () => {
    renderIt()
    expect(await screen.findByText('In the last 30 days: web search 2, connected service 1.')).toBeInTheDocument()
    expect(screen.getByText('tide times')).toBeInTheDocument()
    expect(screen.getByText('GitHub')).toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('Keep run records for'), { target: { value: '7' } })
    await waitFor(() => expect(api.setRunRetention).toHaveBeenCalledWith(7))

    vi.spyOn(window, 'confirm').mockReturnValue(true)
    fireEvent.click(screen.getByRole('button', { name: 'Delete run records now' }))
    expect(await screen.findByText(/Deleted 4 run records, 1 automation results, and 2 entries/)).toBeInTheDocument()
  })

  describe('in another language', () => {
    afterEach(async () => {
      await applyLanguage('en')
      localStorage.clear()
    })

    it("shows what the computer itself sent in the app's language (#1271)", async () => {
      vi.mocked(api.listEgress).mockResolvedValue([
        { id: '3', at: '2026-10-01T20:02:00Z', kind: 'web_fetch', destination: 'toskar.ai', detail: 'the AI news', source: 'chat' },
        { id: '4', at: '2026-10-01T20:03:00Z', kind: 'web_fetch', destination: 'huggingface.co', detail: 'training examples and instructions for me/trail', source: 'chat' },
      ] as Awaited<ReturnType<typeof api.listEgress>>)
      await applyLanguage('de')
      renderIt()
      expect(await screen.findByText('die KI-News')).toBeInTheDocument()
      expect(screen.getByText('Trainingsbeispiele und Anweisungen für me/trail')).toBeInTheDocument()
    })

    it('shows its text, counts, and retention choices from the catalog', async () => {
      await applyLanguage('en-XA')
      renderIt()
      expect(await screen.findByText(/^\[!! .*2.*1.* !!\]$/)).toBeInTheDocument() // the 30-day summary
      const keep = screen.getByRole('combobox')
      expect(Array.from((keep as HTMLSelectElement).options).map((o) => o.textContent)).toEqual([
        expect.stringMatching(/^\[!! 7 .* !!\]$/),
        expect.stringMatching(/^\[!! 30 .* !!\]$/),
        expect.stringMatching(/^\[!! 90 .* !!\]$/),
        expect.stringMatching(/^\[!! 1 .* !!\]$/),
        expect.stringMatching(/^\[!! .* !!\]$/),
      ])
    })
  })
})
