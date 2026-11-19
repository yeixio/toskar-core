import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import { api } from '@/lib/api'
import type { Model, ModelStore } from '@/types/api'
import { DiscoverTab } from './DiscoverTab'

vi.mock('@/lib/api', () => ({ api: { getModelStore: vi.fn() } }))

const model = (id: string, display_name: string, tags: string[]): Model =>
  ({ id, display_name, installed: false, capabilities: {}, tags, purpose: tags, size_bytes: 3e9 }) as Model

describe('DiscoverTab search', () => {
  // Regression: toskar-core#1344
  it('lists each tested match once, with no empty picks', async () => {
    vi.mocked(api.getModelStore).mockResolvedValue({ live: true, shelves: [] } as unknown as ModelStore)
    const models = [model('qwen3.5-4b', 'Qwen 3.5 4B', ['general', 'vision', 'fast']), model('llama', 'Llama 3.2 1B', ['general', 'fast'])]
    render(
      <MemoryRouter>
        <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
          <DiscoverTab models={models} fits={{}} winners={[]} progress={{}} search="Qwen 3.5 4B" onInstall={() => undefined} onBrowseAll={() => undefined} />
        </QueryClientProvider>
      </MemoryRouter>,
    )
    expect(await screen.findByRole('heading', { name: 'Tested by Toskar' })).toBeInTheDocument()
    expect(screen.getAllByRole('heading', { name: 'Qwen 3.5 4B' })).toHaveLength(1)
    expect(screen.queryByText('No recommendations available yet.')).not.toBeVisible()
  })
})
