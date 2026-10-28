import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { api } from '@/lib/api'
import { PromptCachePanel } from './PromptCachePanel'

vi.mock('@/lib/api', () => ({ api: { getPromptCache: vi.fn(), getModels: vi.fn().mockResolvedValue([{ id: 'llama-3.2-3b-q4', display_name: 'Llama 3.2 3B' }]) } }))

function renderPanel() {
  return render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <PromptCachePanel />
    </QueryClientProvider>,
  )
}

describe('PromptCachePanel', () => {
  // A model the catalog no longer lists shows its id.
  it('shows how much each model skipped, and warns when it is low', async () => {
    vi.mocked(api.getPromptCache).mockResolvedValue({
      since: '2026-10-20T00:00:00Z',
      models: [
        { model_id: 'llama-3.2-3b-q4', turns: 12, prompt_tokens: 20000, cached_tokens: 18400, reuse: 0.92 },
        { model_id: 'gemma-3-4b-q4', turns: 6, prompt_tokens: 9000, cached_tokens: 900, reuse: 0.1, low: true },
      ],
    })
    renderPanel()
    const good = (await screen.findByText('Llama 3.2 3B')).closest('tr')!
    expect(within(good).getByText('92%')).toBeInTheDocument()
    expect(within(good).getByText('12 turns · 18,400 of 20,000 tokens')).toBeInTheDocument()
    expect(within(good).queryByText(/^Low:/)).not.toBeInTheDocument()
    const low = screen.getByText('gemma-3-4b-q4').closest('tr')!
    expect(within(low).getByText('10%')).toBeInTheDocument()
    expect(within(low).getByText(/^Low:/)).toBeInTheDocument()
  })

  // An older daemon, or a week without later turns, must not break the page.
  it('renders nothing without models', async () => {
    vi.mocked(api.getPromptCache).mockResolvedValue({} as never)
    const { container } = renderPanel()
    await waitFor(() => expect(api.getPromptCache).toHaveBeenCalled())
    expect(container).toBeEmptyDOMElement()
  })
})
