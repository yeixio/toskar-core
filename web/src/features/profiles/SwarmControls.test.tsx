import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { api } from '@/lib/api'
import { useUIStore } from '@/stores/uiStore'
import type { AIProfile, Model } from '@/types/api'
import { ProfilesPage } from './ProfilesPage'
import { cleanOrchestration } from './OrchestrationControls'

// The Swarm strategy and its controls in a profile (#1135).
vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      getProfiles: vi.fn(),
      getModels: vi.fn(),
      getNodes: vi.fn(),
      listKnowledge: vi.fn(),
      updateProfile: vi.fn(),
    },
  }
})

const general: AIProfile = {
  id: 'general-assistant',
  name: 'General',
  purpose: 'general',
  orchestrator_id: 'simple',
  roles: [{ role: 'assistant', model_id: '', required: true }],
  node_policy: { mode: 'automatic' },
  tools: [],
}

const models = [
  { id: 'qwen', display_name: 'Qwen', installed: true },
  { id: 'gemma', display_name: 'Gemma', installed: true },
  { id: 'llama', display_name: 'Llama', installed: true },
] as Model[]

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <ProfilesPage />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

async function openBehavior() {
  renderPage()
  fireEvent.click(await screen.findByRole('button', { name: 'Edit' }))
  fireEvent.click(screen.getByRole('tab', { name: 'Strategy and computers' }))
}

describe('Swarm strategy (#1135)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    useUIStore.setState({ advancedMode: true })
    vi.mocked(api.getProfiles).mockResolvedValue([general])
    vi.mocked(api.getModels).mockResolvedValue(models)
    vi.mocked(api.getNodes).mockResolvedValue([])
    vi.mocked(api.listKnowledge).mockResolvedValue([])
    vi.mocked(api.updateProfile).mockImplementation(async (_id, p) => p as AIProfile)
  })

  it('offers Swarm beside the other strategies, with its controls only when chosen', async () => {
    await openBehavior()
    const strategy = screen.getByLabelText('Strategy')
    expect(within(strategy).getAllByRole('option').map((o) => o.textContent)).toEqual([
      'Auto',
      'Single model',
      'Planner + workers',
      'Team',
      'Swarm',
    ])
    expect(screen.queryByRole('group', { name: 'Swarm' })).toBeNull()
    fireEvent.change(strategy, { target: { value: 'swarm' } })
    expect(screen.getByText(/Several agents work at once/)).toBeInTheDocument()
    const controls = screen.getByRole('group', { name: 'Swarm' })
    expect(within(controls).getByLabelText(/^Agents/)).toHaveAttribute('min', '2')
    expect(within(controls).getByLabelText(/^Agents/)).toHaveAttribute('max', '8')
    expect(within(controls).getByLabelText(/^Tokens/)).toHaveAttribute('max', '4000000')
    expect(within(controls).getByLabelText(/^Minutes/)).toHaveAttribute('max', '240')
  })

  it('saves a race with its models in order and its budgets', async () => {
    await openBehavior()
    fireEvent.change(screen.getByLabelText('Strategy'), { target: { value: 'swarm' } })
    const controls = screen.getByRole('group', { name: 'Swarm' })
    fireEvent.change(within(controls).getByLabelText(/^Mode/), { target: { value: 'race' } })
    expect(within(controls).getByText('A race needs at least two models.')).toBeInTheDocument()
    fireEvent.change(within(controls).getByLabelText('Add a model'), { target: { value: 'gemma' } })
    fireEvent.change(within(controls).getByLabelText('Add a model'), { target: { value: 'qwen' } })
    expect(within(controls).queryByText('A race needs at least two models.')).toBeNull()
    // Qwen goes first.
    fireEvent.click(within(controls).getByRole('button', { name: 'Move Qwen up' }))
    fireEvent.change(within(controls).getByLabelText(/^Agents/), { target: { value: '3' } })
    fireEvent.change(within(controls).getByLabelText(/^Minutes/), { target: { value: '20' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save profile' }))
    await waitFor(() => expect(api.updateProfile).toHaveBeenCalled())
    expect(vi.mocked(api.updateProfile).mock.calls[0][1].orchestration).toEqual({
      strategy: 'swarm',
      swarm: { mode: 'race', models: ['qwen', 'gemma'], agents: 3, minutes: 20 },
    })
  })

  it('removes a model', async () => {
    await openBehavior()
    fireEvent.change(screen.getByLabelText('Strategy'), { target: { value: 'swarm' } })
    const controls = screen.getByRole('group', { name: 'Swarm' })
    fireEvent.change(within(controls).getByLabelText('Add a model'), { target: { value: 'llama' } })
    fireEvent.click(within(controls).getByRole('button', { name: 'Remove Llama' }))
    expect(within(controls).queryByRole('button', { name: 'Remove Llama' })).toBeNull()
  })

  it('drops empty swarm controls', () => {
    expect(cleanOrchestration({ strategy: 'swarm', swarm: { mode: '', models: [], agents: 0 } })).toEqual({ strategy: 'swarm' })
    expect(cleanOrchestration({ swarm: { tokens: 50000 } })).toEqual({ swarm: { tokens: 50000 } })
  })
})
