import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import { ApiError } from '@/lib/api'
import { ShownError, shownError, shownOr } from './ShownError'

vi.mock('@/lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/api')>()),
  api: {
    getReport: vi.fn().mockResolvedValue({ version: '1.9.0', contract: '1.36', os: 'darwin', arch: 'arm64' }),
    getSettings: vi.fn().mockResolvedValue({ problem_reports: true }),
  },
}))

function show(node: React.ReactNode) {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <MemoryRouter>{node}</MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('ShownError (#588)', () => {
  it('explains a record’s error from its code, and reports the code', () => {
    show(<ShownError error='model "qwen3" not installed' code="MODEL_NOT_INSTALLED" details={{ model_id: 'qwen3' }} />)
    expect(screen.getByText("qwen3 isn't installed.")).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Report this' }))
    const send = screen.getByRole('link', { name: 'Or send it by email' })
    expect(send.getAttribute('href')).toContain('MODEL_NOT_INSTALLED')
  })

  it('shows the text of an error without a code', () => {
    show(<ShownError error="the page moved" />)
    expect(screen.getByText('the page moved')).toBeInTheDocument()
  })

  it('reads a failed request’s text, code, and values', () => {
    const error = new ApiError(404, 'model "qwen3" not installed', 'MODEL_NOT_INSTALLED', { model_id: 'qwen3' })
    expect(shownError(error)).toEqual({ error: 'model "qwen3" not installed', code: 'MODEL_NOT_INSTALLED', details: { model_id: 'qwen3' } })
    expect(shownError(new Error('boom'))).toEqual({ error: 'boom', code: undefined, details: undefined })
  })

  it('uses the page’s own text for an error that has none', () => {
    expect(shownOr(new Error(''), 'Couldn’t save.')).toEqual({ error: 'Couldn’t save.' })
    expect(shownOr('not an error', 'Couldn’t save.')).toEqual({ error: 'Couldn’t save.' })
    expect(shownOr(new ApiError(500, 'disk full', 'SETTINGS_SAVE_FAILED'), 'x').code).toBe('SETTINGS_SAVE_FAILED')
  })
})
