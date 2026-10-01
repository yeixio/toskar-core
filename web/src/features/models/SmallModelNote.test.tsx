import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import type { Model, ModelFit } from '@/types/api'
import { SmallModelNote } from './SmallModelNote'
import { isSmallModel, largerAlternative, parameterBillions } from './modelPresentation'

const model = (id: string, parameters: string, extra: Partial<Model> = {}): Model =>
  ({ id, display_name: id, parameters, installed: false, purpose: ['general'], memory_needed_bytes: 1, ...extra }) as Model

describe('small models', () => {
  it('reads sizes and calls models under 4B small', () => {
    expect(parameterBillions({ parameters: '3.8B' })).toBe(3.8)
    expect(parameterBillions({ parameters: '500M' })).toBe(0.5)
    expect(parameterBillions({ parameters: 'big' })).toBeNull()
    expect(isSmallModel({ parameters: '1B' })).toBe(true)
    expect(isSmallModel({ parameters: '7B' })).toBe(false)
    expect(isSmallModel({ parameters: undefined })).toBe(false)
  })

  it('suggests an installed larger model first, then the largest that fits', () => {
    const fits = { q7: { label: 'good' }, q14: { label: 'too_large' }, v7: { label: 'excellent' } } as unknown as Record<string, ModelFit>
    const list = [
      model('tiny', '1B'),
      model('q7', '7B', { memory_needed_bytes: 6 }),
      model('q14', '14B', { memory_needed_bytes: 12 }),
      model('v7', '7B', { memory_needed_bytes: 7, tags: ['vision'] }),
    ]
    expect(largerAlternative(list, fits)?.id).toBe('q7')
    list[2] = { ...list[2], installed: true }
    expect(largerAlternative(list, fits)?.id).toBe('q14')
  })

  it('shows the note only for small models, with an install button for a larger one', () => {
    const onInstall = vi.fn()
    const { rerender } = render(
      <SmallModelNote model={model('tiny', '1B')} alternative={model('Qwen 7B', '7B')} onInstallAlternative={onInstall} />,
    )
    expect(screen.getByRole('note')).toHaveTextContent('can mix up facts and numbers')
    fireEvent.click(screen.getByRole('button', { name: 'Install Qwen 7B' }))
    expect(onInstall).toHaveBeenCalledWith('Qwen 7B')

    rerender(<SmallModelNote model={model('big', '7B')} />)
    expect(screen.queryByRole('note')).not.toBeInTheDocument()
  })
})
