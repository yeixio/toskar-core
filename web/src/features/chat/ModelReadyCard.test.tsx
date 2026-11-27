import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import { api } from '@/lib/api'
import type { MainModelView, Model } from '@/types/api'
import { ModelReadyCard } from './ModelReadyCard'

const gb = 1024 ** 3
const model = (extra: Partial<MainModelView> = {}): MainModelView => ({
  model_id: 'qwen3.8-8b-q4',
  name: 'Qwen 3.8 8B',
  memory_bytes: 6 * gb,
  memory_total_bytes: 32 * gb,
  fits: true,
  always_running: false,
  ...extra,
})

// The choice of how the main chat model stays ready (#1472).
describe('ModelReadyCard', () => {
  it('names the model, what Always ready costs, and saves the choice', () => {
    const onChoose = vi.fn()
    render(
      <MemoryRouter>
        <ModelReadyCard model={model()} onChoose={onChoose} />
      </MemoryRouter>,
    )
    expect(screen.getByRole('heading').textContent).toContain('Qwen 3.8 8B')
    const always = screen.getByRole('button', { name: /Always ready/ })
    expect(always.textContent).toMatch(/6 GB of this computer's 32 GB/)
    fireEvent.click(always)
    fireEvent.click(screen.getByRole('button', { name: /Let Toskar manage/ }))
    expect(onChoose.mock.calls).toEqual([['always'], ['managed']])
    expect(screen.getByRole('link').getAttribute('href')).toBe('/settings/models#model-lifecycle')
  })

  it('says a big model is freed when idle, so a reply after a break waits', () => {
    render(
      <MemoryRouter>
        <ModelReadyCard model={model({ fits: false, memory_total_bytes: 0 })} onChoose={() => undefined} />
      </MemoryRouter>,
    )
    expect(screen.getByRole('button', { name: /Let Toskar manage/ }).textContent).toMatch(/waits while it loads/)
    expect(screen.getByRole('button', { name: /Always ready/ }).textContent).not.toMatch(/this computer/)
  })

  it('says Auto chose the model, and Change picks another', async () => {
    vi.spyOn(api, 'getModels').mockResolvedValue([
      { id: 'qwen3.8-8b-q4', display_name: 'Qwen 3.8 8B', installed: true, memory_needed_bytes: 6 * gb },
      { id: 'gemma-12b', display_name: 'Gemma 12B', installed: true, memory_needed_bytes: 9 * gb },
      { id: 'nomic', display_name: 'Nomic', installed: true, support_role: 'embedding' },
    ] as Model[])
    const onPick = vi.fn()
    render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter>
          <ModelReadyCard model={model()} onChoose={() => undefined} onPick={onPick} />
        </MemoryRouter>
      </QueryClientProvider>,
    )
    expect(screen.getByText(/Chosen by Auto/)).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Change' }))
    const select = (await screen.findByRole('combobox')) as HTMLSelectElement
    await screen.findByRole('option', { name: /Gemma 12B/ })
    expect(screen.queryByRole('option', { name: /Nomic/ })).toBeNull()
    fireEvent.change(select, { target: { value: 'gemma-12b' } })
    expect(onPick).toHaveBeenCalledWith('gemma-12b')
  })
})
