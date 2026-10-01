import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { api } from '@/lib/api'
import { CachePanel } from './CachePanel'

vi.mock('@/lib/api', () => ({ api: { listCaches: vi.fn(), clearCache: vi.fn() } }))

describe('CachePanel', () => {
  it('shows each policy and clears in-memory caches', async () => {
    vi.mocked(api.listCaches).mockResolvedValue([
      { name: 'web_search', label: 'Web searches', key: 'the search query', ttl: '15m0s', invalidation: 'age', scope: 'this computer', privacy: 'personal', entries: 3, hits: 5, misses: 3, evictions: 0 },
      { name: 'knowledge_index', label: 'Knowledge search index', key: 'source and passage', ttl: 'kept until invalidated', scope: 'this computer', privacy: 'personal', persistent: true, entries: 120, hits: 0, misses: 0, evictions: 0 },
    ])
    vi.mocked(api.clearCache).mockResolvedValue(null)
    render(
      <QueryClientProvider client={new QueryClient()}>
        <CachePanel />
      </QueryClientProvider>,
    )
    expect(await screen.findByText(/kept 15m0s/)).toBeInTheDocument()
    expect(screen.getByText(/5 hits · 3 misses/)).toBeInTheDocument()
    expect(screen.getAllByRole('button', { name: 'Clear' })).toHaveLength(1)
    fireEvent.click(screen.getByRole('button', { name: 'Clear' }))
    await waitFor(() => expect(api.clearCache).toHaveBeenCalledWith('web_search'))
  })
})
