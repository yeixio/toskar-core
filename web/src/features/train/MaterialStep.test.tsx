import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/lib/api'
import type { ClassifyResult, SpecializedAIView } from '@/types/api'
import { MaterialStep } from './steps/MaterialStep'

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    api: { ...actual.api, classifyMaterial: vi.fn(), addMaterial: vi.fn(), getConversations: vi.fn(), deleteMaterial: vi.fn() },
  }
})

const view: SpecializedAIView = {
  id: 'ai-1',
  slug: 'tire-bot',
  name: 'Tire Bot',
  goal: '',
  instructions: 'You are Tire Bot.',
  base_model_id: 'm',
  preset: 'balanced',
  knowledge_sources: [],
  deployed_revision: 0,
  created_at: '',
  updated_at: '',
  model_id: 'sai:tire-bot',
  materials: [],
  dataset: { total: 0, usable: 0, excluded: 0, flagged: {}, tokens: 0, p95_tokens: 0 },
  revisions: [],
  jobs: [],
  eval_runs: [],
  test_prompts: [],
  deployable_revisions: [],
}

const knowledge: ClassifyResult = {
  use: 'knowledge',
  recommendation: {
    use: 'knowledge',
    reasons: ['This is a table of 2 rows, not conversation examples.'],
    example_count: 0,
    can_train: false,
  },
}

function renderStep() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <MaterialStep view={view} onNext={() => {}} />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

const settled = { timeout: 4000 }

describe('MaterialStep', () => {
  beforeEach(() => {
    vi.mocked(api.classifyMaterial).mockReset()
    vi.mocked(api.addMaterial).mockReset()
  })

  it('shows the recommendation and blocks training on a table with no examples', async () => {
    vi.mocked(api.classifyMaterial).mockImplementation(async (_f, _t, use) =>
      use === 'training'
        ? { ...knowledge, use: 'training', error: 'no training examples were found in this material' }
        : knowledge,
    )
    renderStep()
    expect(screen.getByText('Train how your AI behaves')).toBeInTheDocument()
    expect(screen.getByText('Connect what your AI knows')).toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('Material'), { target: { value: 'sku,price\nA1,9.99\n' } })
    // Classifying waits 400 ms after typing stops, which a busy runner can
    // stretch past waitFor's default second.
    await waitFor(() => expect(screen.getByText(/not conversation examples/)).toBeInTheDocument(), settled)
    expect(screen.getByRole('button', { name: 'Add material' })).toBeEnabled()

    fireEvent.click(screen.getByRole('radio', { name: 'Training' }))
    await waitFor(() => expect(screen.getByText(/no training examples were found/)).toBeInTheDocument(), settled)
    expect(screen.getByRole('button', { name: 'Add material' })).toBeDisabled()
  })

  it('adds material with the chosen use', async () => {
    vi.mocked(api.classifyMaterial).mockResolvedValue(knowledge)
    vi.mocked(api.addMaterial).mockResolvedValue(null)
    renderStep()
    fireEvent.change(screen.getByLabelText('Material'), { target: { value: 'sku,price\nA1,9.99\n' } })
    await waitFor(() => expect(screen.getByRole('button', { name: 'Add material' })).toBeEnabled(), settled)
    fireEvent.click(screen.getByRole('button', { name: 'Add material' }))
    await waitFor(() =>
      expect(api.addMaterial).toHaveBeenCalledWith('ai-1', { filename: 'pasted.txt', text: 'sku,price\nA1,9.99\n', use: 'knowledge' }),
    )
  })
})
