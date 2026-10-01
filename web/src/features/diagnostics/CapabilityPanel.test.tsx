import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { api } from '@/lib/api'
import { CapabilityPanel } from './CapabilityPanel'

vi.mock('@/lib/api', () => ({ api: { getCapabilities: vi.fn() } }))

describe('CapabilityPanel', () => {
  it('lists abilities with how they work or what is missing', async () => {
    vi.mocked(api.getCapabilities).mockResolvedValue({
      at: '2026-10-01T00:00:00Z',
      models: [{ id: 'm', name: 'Llama', running: true, on: ['This Mac'] }],
      nodes: [{ id: 'n', name: 'This Mac', local: true, online: true }],
      tools: [{ id: 'internet.search', name: 'Web Search', source: 'builtin', enabled: true }],
      connectors: [],
      providers: [{ id: 'llamacpp', name: 'llama.cpp', kind: 'runtime', status: 'installed', healthy: true }],
      artifacts: { count: 3, bytes: 3 << 20 },
      abilities: [
        { id: 'web', label: 'Search the web and read pages', available: true, via: ['Web Search'] },
        { id: 'image_generation', label: 'Generate images', available: false, note: 'No image model or image tool is installed.' },
      ],
    })
    render(
      <QueryClientProvider client={new QueryClient()}>
        <CapabilityPanel />
      </QueryClientProvider>,
    )
    expect(await screen.findByText('Search the web and read pages')).toBeInTheDocument()
    expect(screen.getByText(/Via Web Search/)).toBeInTheDocument()
    expect(screen.getByText(/No image model or image tool is installed/)).toBeInTheDocument()
    expect(screen.getByText(/1 models installed · 1 of 1 computers online · 1 tools/)).toBeInTheDocument()
  })
})
