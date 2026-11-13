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

// Agents at once is 1 to 4 per model, in Advanced mode only, with a line
// that says what it does; a model on its way to answering shows that it
// reloads (#1135).
describe('InstalledTab agents at once', () => {
  afterEach(() => useUIStore.setState({ advancedMode: false }))

  it('picks how many agents a swarm runs on the model at once', () => {
    useUIStore.setState({ advancedMode: true })
    const onSlots = vi.fn()
    renderTab({ models: [{ ...qwen, slots: 1 }], onSlots })
    fireEvent.click(screen.getByRole('button', { name: 'More actions for Qwen3.5 35B' }))
    const group = screen.getByRole('group', { name: 'Agents at once' })
    expect(group).toHaveTextContent('Lets a swarm run this many agents on this model at the same time.')
    expect(screen.getAllByRole('menuitemradio').map((b) => b.textContent)).toEqual(['✓1', '2', '3', '4'])
    expect(screen.getByRole('menuitemradio', { name: '1' })).toHaveAttribute('aria-checked', 'true')
    fireEvent.click(screen.getByRole('menuitemradio', { name: '2' }))
    expect(onSlots).toHaveBeenCalledWith('qwen3.5-35b', 2)
  })

  it('says a loaded model reloads, and does nothing for the number it has', () => {
    useUIStore.setState({ advancedMode: true })
    const onSlots = vi.fn()
    const running = [{ model_id: 'qwen3.5-35b', instance_id: 'i1', status: 'running' }] as Parameters<typeof InstalledTab>[0]['running']
    renderTab({ models: [{ ...qwen, slots: 2 }], running, onSlots })
    fireEvent.click(screen.getByRole('button', { name: 'More actions for Qwen3.5 35B' }))
    expect(screen.getByRole('group', { name: 'Agents at once' })).toHaveTextContent('reloads on its next message')
    fireEvent.click(screen.getByRole('menuitemradio', { name: '2' }))
    expect(onSlots).not.toHaveBeenCalled()
  })

  it('has no such choice outside Advanced mode', () => {
    renderTab({ models: [{ ...qwen, slots: 1 }], onSlots: vi.fn() })
    fireEvent.click(screen.getByRole('button', { name: 'More actions for Qwen3.5 35B' }))
    expect(screen.queryByRole('group', { name: 'Agents at once' })).not.toBeInTheDocument()
  })
})
