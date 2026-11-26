import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import type { MainModelView } from '@/types/api'
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
})
