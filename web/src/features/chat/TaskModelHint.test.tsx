import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it } from 'vitest'
import type { FitLabel, Model, ModelsFitResponse } from '@/types/api'
import { TaskModelHint } from './TaskModelHint'
import { taskModelSuggestion } from './taskModel'

const model = (id: string, extra: Partial<Model> = {}): Model =>
  ({ id, display_name: id, capabilities: { tool_calling: true, vision: false, coding: false }, installed: false, ...extra }) as Model

const big = model('qwen3.8-27b-q4', { display_name: 'Qwen 3.8 27B', tags: ['tasks'], size_bytes: 19603117536 })

// This computer first, as /models/fit lists it, then a connected one.
const fits = (here: FitLabel, there: FitLabel = 'excellent'): ModelsFitResponse[] => [
  { node_id: 'here', memory_bytes: 24e9, winners: [], fits: [{ model_id: big.id, label: here, expected_memory_bytes: 23e9 }] },
  { node_id: 'there', memory_bytes: 128e9, winners: [], fits: [{ model_id: big.id, label: there, expected_memory_bytes: 23e9 }] },
]

describe('TaskModelHint', () => {
  it('suggests the model measured to finish tasks, with how it fits this computer', () => {
    render(
      <MemoryRouter>
        <TaskModelHint models={[model('qwen2.5-7b-q4', { installed: true }), big]} fits={fits('good', 'too_large')} />
      </MemoryRouter>,
    )
    expect(screen.getByText(/goes much better with Qwen 3\.8 27B/).textContent).toMatch(/\(18\.3 GB, Good fit here\)/)
    expect(screen.getByRole('link', { name: 'Install it on the Models page' }).getAttribute('href')).toBe('/models')
  })

  it('says when that model needs more than this computer has, whatever a connected one has, with no install link', () => {
    render(
      <MemoryRouter>
        <TaskModelHint models={[big]} fits={fits('too_large')} />
      </MemoryRouter>,
    )
    expect(screen.getByText(/needs more than this computer has/)).toBeTruthy()
    expect(screen.queryByRole('link')).toBeNull()
  })

  it('says nothing once a task model is installed, or when the catalog has none', () => {
    expect(taskModelSuggestion([{ ...big, installed: true }], fits('good'))).toBeNull()
    expect(taskModelSuggestion([model('qwen2.5-7b-q4', { installed: true })], fits('good'))).toBeNull()
  })
})
