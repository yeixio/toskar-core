import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import type { Model } from '@/types/api'
import { InstalledTab } from './InstalledTab'

vi.mock('./ratings', () => ({ CommunityScore: () => null, RateButton: () => null }))

const qwen = { id: 'qwen3.5-35b', display_name: 'Qwen3.5 35B', installed: true, status: 'installed', size_bytes: 22e9 } as Model

function renderTab(props: Partial<Parameters<typeof InstalledTab>[0]>) {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <MemoryRouter>
        <InstalledTab
          models={[qwen]}
          running={[]}
          profiles={[]}
          progress={{}}
          nodes={[]}
          showManualControls
          search=""
          onStart={vi.fn()}
          onStop={vi.fn()}
          onDelete={vi.fn()}
          {...props}
        />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

// Start says it's working, and says why it failed, instead of doing nothing (#610).
describe('InstalledTab start', () => {
  it('shows Starting while a model starts', () => {
    renderTab({ startingId: 'qwen3.5-35b' })
    expect(screen.getByRole('button', { name: 'Starting…' })).toBeDisabled()
  })

  it("shows why a start failed under that model", () => {
    renderTab({ startError: { id: 'qwen3.5-35b', error: "llama.cpp couldn't be set up, so the model can't start (no network)." } })
    expect(screen.getByRole('alert')).toHaveTextContent("llama.cpp couldn't be set up")
    expect(screen.getByRole('button', { name: 'Start' })).toBeEnabled()
  })
})
