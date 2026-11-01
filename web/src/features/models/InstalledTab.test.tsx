import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { Model } from '@/types/api'
import { useUIStore } from '@/stores/uiStore'
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

// Guessing ahead (speculative decoding) is Auto or Off per model, in Advanced
// mode only (#835).
describe('InstalledTab guess ahead', () => {
  it('turns guessing ahead off for a model, in Advanced mode', () => {
    useUIStore.setState({ advancedMode: true })
    const onSpeculation = vi.fn()
    renderTab({ models: [{ ...qwen, speculation: 'auto' }], onSpeculation })
    fireEvent.click(screen.getByRole('button', { name: 'More actions for Qwen3.5 35B' }))
    const item = screen.getByRole('menuitemcheckbox', { name: 'Guess ahead' })
    expect(item).toHaveAttribute('aria-checked', 'true')
    fireEvent.click(item)
    expect(onSpeculation).toHaveBeenCalledWith('qwen3.5-35b', 'off')
    useUIStore.setState({ advancedMode: false })
  })

  it('has no such choice outside Advanced mode', () => {
    renderTab({ models: [{ ...qwen, speculation: 'auto' }], onSpeculation: vi.fn() })
    fireEvent.click(screen.getByRole('button', { name: 'More actions for Qwen3.5 35B' }))
    expect(screen.queryByRole('menuitemcheckbox')).not.toBeInTheDocument()
  })
})

// The context window is Auto or a size per model, in Advanced mode only; a
// size the model won't fit at can't be chosen (#962).
describe('InstalledTab context window', () => {
  afterEach(() => useUIStore.setState({ advancedMode: false }))
  const withWindows = {
    ...qwen,
    context_window: 32768,
    context_options: [
      { tokens: 8192, fit: 'excellent' as const },
      { tokens: 32768, fit: 'excellent' as const },
      { tokens: 65536, fit: 'tight' as const },
      { tokens: 131072, fit: 'unsupported' as const },
    ],
  }

  it('picks a size, shows Auto’s, and marks the fit', () => {
    useUIStore.setState({ advancedMode: true })
    const onContextWindow = vi.fn()
    renderTab({ models: [withWindows], onContextWindow })
    fireEvent.click(screen.getByRole('button', { name: 'More actions for Qwen3.5 35B' }))
    expect(screen.getByRole('menuitemradio', { name: 'Auto · 32K' })).toHaveAttribute('aria-checked', 'true')
    expect(screen.getByRole('menuitemradio', { name: '64K · Tight fit' })).toBeEnabled()
    expect(screen.getByRole('menuitemradio', { name: '128K · Unsupported' })).toBeDisabled()
    fireEvent.click(screen.getByRole('menuitemradio', { name: '64K · Tight fit' }))
    expect(onContextWindow).toHaveBeenCalledWith('qwen3.5-35b', 65536)
    useUIStore.setState({ advancedMode: false })
  })

  it('goes back to Auto from a chosen size', () => {
    useUIStore.setState({ advancedMode: true })
    const onContextWindow = vi.fn()
    renderTab({ models: [{ ...withWindows, context_window: 65536, context_choice: 65536 }], onContextWindow })
    fireEvent.click(screen.getByRole('button', { name: 'More actions for Qwen3.5 35B' }))
    expect(screen.getByRole('menuitemradio', { name: '64K · Tight fit' })).toHaveAttribute('aria-checked', 'true')
    fireEvent.click(screen.getByRole('menuitemradio', { name: 'Auto' }))
    expect(onContextWindow).toHaveBeenCalledWith('qwen3.5-35b', 0)
    useUIStore.setState({ advancedMode: false })
  })

  it('has no such choice outside Advanced mode', () => {
    renderTab({ models: [withWindows], onContextWindow: vi.fn() })
    fireEvent.click(screen.getByRole('button', { name: 'More actions for Qwen3.5 35B' }))
    expect(screen.queryByRole('menuitemradio')).not.toBeInTheDocument()
  })
})
