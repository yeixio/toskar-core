import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, within } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { applyLanguage } from '@/i18n'
import { api } from '@/lib/api'
import type { QualityResults } from '@/types/api'
import { ResultsTab } from './ResultsTab'

vi.mock('@/lib/api', () => ({ api: { getQualityResults: vi.fn() } }))

const data: QualityResults = {
  live: true,
  results: [
    { model: 'a', name: 'Alpha', date: '2026-10-31', hardware: 'Card', method_version: 0, source: 'https://example.com/run', scores: { quality: { passed: 60, of: 64 } }, seconds_per_question: 2 },
    { model: 'b', name: 'Beta', date: '2026-11-02', hardware: 'Mac', method_version: 1, scores: { quality: { passed: 64, of: 64 }, future_set: { passed: 1, of: 2 } } },
    { model: 'c', name: 'Gamma', date: '2026-11-01', hardware: 'Mac', method_version: 1, submitted: true, submitted_by: 'someone', scores: { fix_test: { passed: 3, of: 4 } } },
  ],
}

function show() {
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <ResultsTab />
    </QueryClientProvider>,
  )
}

const order = () => screen.getAllByRole('rowheader').map((c) => c.textContent)

describe('ResultsTab', () => {
  beforeEach(async () => {
    await applyLanguage('en')
    vi.mocked(api.getQualityResults).mockReset()
    vi.mocked(api.getQualityResults).mockResolvedValue(data)
  })

  it('lists each result with its computer, date, version, and source, newest first', async () => {
    show()
    expect(await screen.findByRole('table')).toBeInTheDocument()
    expect(order()[0]).toContain('Beta')
    const alpha = within(screen.getByRole('rowheader', { name: 'Alpha' }).closest('tr')!)
    expect(alpha.getByText('60 of 64')).toBeInTheDocument()
    expect(alpha.getByText('Card')).toBeInTheDocument()
    expect(alpha.getByRole('link')).toHaveAttribute('href', 'https://example.com/run')
  })

  it('sorts by a column, with rows that have no score last either way', async () => {
    show()
    fireEvent.click(await screen.findByRole('button', { name: 'Quality' }))
    expect(order().map((n) => n?.slice(0, 4))).toEqual(['Beta', 'Alph', 'Gamm'])
    fireEvent.click(screen.getByRole('button', { name: /Quality/ }))
    expect(order().map((n) => n?.slice(0, 4))).toEqual(['Alph', 'Beta', 'Gamm'])
  })

  it('filters by computer', async () => {
    show()
    fireEvent.change(await screen.findByRole('combobox'), { target: { value: 'Card' } })
    expect(order()).toEqual(['Alpha'])
  })

  it('says different computers and versions are not the same test, until only one is shown', async () => {
    show()
    expect(await screen.findByText(/aren't the same test/)).toBeInTheDocument()
    fireEvent.change(screen.getByRole('combobox'), { target: { value: 'Card' } })
    expect(screen.queryByText(/aren't the same test/)).not.toBeInTheDocument()
  })

  it('marks a submitted result, shows a set this build does not know, and invites people in', async () => {
    show()
    expect(await screen.findByText('someone')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'future_set' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /How this is measured/ })).toHaveAttribute('href', expect.stringContaining('docs/quality.md'))
    expect(screen.getByRole('link', { name: /Suggest a change/ })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /Add your computer/ })).toBeInTheDocument()
  })

  it("says so when the results can't be read", async () => {
    vi.mocked(api.getQualityResults).mockResolvedValue({ live: false, results: [] })
    show()
    expect(await screen.findByText(/Couldn't read the results/)).toBeInTheDocument()
  })

  it('gives a complete row a Score and shows how fast it ran', async () => {
    vi.mocked(api.getQualityResults).mockResolvedValue({
      live: true,
      results: [
        {
          model: 'd',
          name: 'Delta',
          date: '2026-11-02',
          hardware: 'Card',
          method_version: 1,
          runs: 'partial',
          scores: { quality: { passed: 60, of: 64 }, topics: { passed: 20, of: 22 }, files: { passed: 20, of: 21 }, reasoning: { passed: 32, of: 39 } },
          performance: { prompt_tokens: 4000, prompt_tokens_per_second: 2112.4, tokens_per_second: 41.3, first_token_ms: 2270, effective_tokens_per_second: 21.5, gpu_layers: 15, gpu_layers_total: 65 },
        },
        { model: 'e', name: 'Echo', date: '2026-11-01', hardware: 'Card', method_version: 1, scores: { quality: { passed: 60, of: 64 } } },
        { model: 'f', name: 'Foxtrot', date: '2026-11-01', hardware: 'Card', method_version: 1, runs: 'no', why: 'out_of_memory', scores: {} },
      ],
    })
    show()
    const delta = within((await screen.findByRole('rowheader', { name: 'Delta' })).closest('tr')!)
    expect(delta.getByText('90 (v1)')).toBeInTheDocument()
    expect(delta.getByText('Partly')).toBeInTheDocument()
    expect(within(screen.getByRole('rowheader', { name: 'Foxtrot' }).closest('tr')!).getByText("Doesn't run")).toBeInTheDocument()
    expect(delta.getByText('41.3')).toBeInTheDocument()
    expect(delta.getByText('2,112')).toBeInTheDocument()
    expect(delta.getByText('2.3 s')).toBeInTheDocument()
    const echo = within(screen.getByRole('rowheader', { name: 'Echo' }).closest('tr')!)
    expect(echo.queryByText(/\(v1\)/)).not.toBeInTheDocument()
  })
})
