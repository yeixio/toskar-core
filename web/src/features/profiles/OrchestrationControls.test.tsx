import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { OrchestrationControls, cleanOrchestration } from './OrchestrationControls'

describe('OrchestrationControls', () => {
  it('edits controls and keeps blanks as defaults', () => {
    const onChange = vi.fn()
    render(<OrchestrationControls value={{ effort: 'balanced' }} onChange={onChange} />)
    expect(screen.getByLabelText(/^Reasoning level/)).toHaveValue('balanced')
    fireEvent.change(screen.getByLabelText(/^Verification/), { target: { value: 'off' } })
    expect(onChange).toHaveBeenLastCalledWith({ effort: 'balanced', verification: 'off' })
    fireEvent.change(screen.getByLabelText(/^Workers/), { target: { value: '3' } })
    expect(onChange).toHaveBeenLastCalledWith({ effort: 'balanced', max_workers: 3 })
  })

  it('drops empty controls', () => {
    expect(cleanOrchestration({ effort: '', planning: 'off', max_workers: 0 })).toEqual({ planning: 'off' })
    expect(cleanOrchestration({ effort: '', max_workers: 0 })).toBeUndefined()
  })
})
