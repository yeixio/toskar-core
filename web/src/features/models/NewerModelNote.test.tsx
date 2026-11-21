import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { applyLanguage } from '@/i18n'
import type { Model, ModelLicense } from '@/types/api'
import { NewerModelNote } from './NewerModelNote'

const license = (id: string, name: string): ModelLicense =>
  ({ id, name, url: '', commercial: 'yes', attribution: false, accept: false, notes: '' }) as ModelLicense

const model = (id: string, extra: Partial<Model> = {}): Model =>
  ({ id, display_name: id, capabilities: {}, installed: false, ...extra }) as Model

describe('NewerModelNote (#593)', () => {
  beforeEach(async () => {
    await applyLanguage('en')
  })

  it('says which model is newer, how it fits, whether its license differs, and installs it', () => {
    const old = model('Qwen 2.5 3B', { installed: true, newer: 'Qwen 3.5 4B', license: license('qwen-research', 'Qwen Research License') })
    const next = model('Qwen 3.5 4B', { license: license('apache-2.0', 'Apache 2.0') })
    const onInstall = vi.fn()
    render(<NewerModelNote model={old} models={[old, next]} fits={{ 'Qwen 3.5 4B': { label: 'good' } as never }} onInstall={onInstall} />)
    expect(screen.getByText('Qwen 3.5 4B is newer.')).toBeInTheDocument()
    expect(screen.getByRole('note')).toHaveTextContent(/Its license is different: Apache 2.0/)
    fireEvent.click(screen.getByRole('button', { name: 'Install Qwen 3.5 4B' }))
    expect(onInstall).toHaveBeenCalledWith('Qwen 3.5 4B')
  })

  it('says nothing once the newer model is installed, or when there is none', () => {
    const old = model('a', { installed: true, newer: 'b' })
    const { container, rerender } = render(<NewerModelNote model={old} models={[old, model('b', { installed: true })]} />)
    expect(container).toBeEmptyDOMElement()
    rerender(<NewerModelNote model={model('c', { installed: true })} models={[]} />)
    expect(container).toBeEmptyDOMElement()
  })
})
