import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { applyLanguage } from '@/i18n'
import { api } from '@/lib/api'
import type { ModelLicense } from '@/types/api'
import { LicenseTermsDialog } from './LicenseTermsDialog'

const gemma: ModelLicense = {
  id: 'gemma',
  name: 'Gemma Terms of Use',
  url: 'https://ai.google.dev/gemma/terms',
  commercial: 'yes',
  accept: true,
  notes: "Business use is allowed; you agree to Google's Gemma terms and their prohibited use policy.",
}

describe('LicenseTermsDialog', () => {
  beforeEach(async () => {
    await applyLanguage('en')
  })

  it('shows the terms, links to them, and says a record is kept', () => {
    render(<LicenseTermsDialog modelName="Gemma 3 4B" license={gemma} onCancel={vi.fn()} onAccept={vi.fn()} />)
    expect(screen.getByRole('heading', { name: "Accept Gemma 3 4B's license" })).toBeInTheDocument()
    expect(screen.getByText(/prohibited use policy/)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Read the Gemma Terms of Use' })).toHaveAttribute('href', gemma.url)
    expect(screen.getByText(/keeps a record/)).toBeInTheDocument()
  })

  // Regression: toskar-core#1338
  it('names the model in the terms, with no placeholder left', () => {
    render(<LicenseTermsDialog modelName="Gemma 3 4B" license={gemma} onCancel={vi.fn()} onAccept={vi.fn()} />)
    expect(screen.getByText(/^Gemma 3 4B comes under the Gemma Terms of Use/)).toBeInTheDocument()
    expect(document.body.textContent).not.toContain('{{')
  })

  it('installs only once the terms are accepted', () => {
    const onAccept = vi.fn()
    const onCancel = vi.fn()
    render(<LicenseTermsDialog modelName="Gemma 3 4B" license={gemma} onCancel={onCancel} onAccept={onAccept} />)
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(onAccept).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'Accept and install' }))
    expect(onAccept).toHaveBeenCalledOnce()
  })
})

describe('api.installModel', () => {
  it('sends the acceptance with the install', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ status: 'started', model_id: 'g' }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
    vi.stubGlobal('fetch', fetchMock)
    await api.installModel('gemma-3-4b-q4', { accept_license: true })
    const [, init] = fetchMock.mock.calls[0]
    expect(JSON.parse(init.body)).toEqual({ accept_license: true })
    vi.unstubAllGlobals()
  })
})
