import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import type { Model, ToolRecord } from '@/types/api'
import { AutomationForm } from './AutomationForm'

vi.mock('@/lib/api', () => ({
  api: new Proxy({}, { get: () => vi.fn(async () => []) }),
}))

const small = { id: 'llama-1b', display_name: 'Llama 3.2 1B', installed: true, capabilities: {} } as Model
const capable = { id: 'qwen-7b', display_name: 'Qwen 7B', installed: true, capabilities: { tool_calling: true } } as Model
const open = { id: 'internet.open', name: 'Open Web Page', description: '', enabled: true, risk: 'read' } as ToolRecord

function renderForm(models: Model[]) {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <MemoryRouter>
        <AutomationForm profiles={[]} models={models} tools={[open]} pending={false} showIdeas={false} onCancel={() => {}} onSubmit={() => {}} />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('AutomationForm QA regressions', () => {
  // Regression: toskar-core#1269
  it("warns when the model can't use the tools the automation needs", () => {
    renderForm([small, capable])
    fireEvent.click(screen.getByRole('checkbox', { name: /Open Web Page/ }))
    const model = screen.getByRole('combobox', { name: 'Model' })
    fireEvent.change(model, { target: { value: 'llama-1b' } })
    expect(screen.getByText(/can't use tools/)).toBeInTheDocument()
    fireEvent.change(model, { target: { value: 'qwen-7b' } })
    expect(screen.queryByText(/can't use tools/)).not.toBeInTheDocument()
  })
})
