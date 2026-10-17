import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/lib/api'
import { ReportProblem } from './ReportProblem'

vi.mock('@/lib/api', () => ({
  api: {
    getReport: vi.fn(),
    getSettings: vi.fn(),
    sendReport: vi.fn(),
  },
}))

function open() {
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { mutations: { retry: false } } })}>
      <MemoryRouter initialEntries={['/chat']}>
        <ReportProblem error="model failed for mike@example.com" code="MODEL_LOAD_FAILED" />
      </MemoryRouter>
    </QueryClientProvider>,
  )
  fireEvent.click(screen.getByRole('button', { name: 'Report this' }))
}

const failure = (code: string) => Object.assign(new Error(code), { code })

describe('ReportProblem (#588)', () => {
  beforeEach(() => {
    vi.mocked(api.getReport).mockResolvedValue({ version: '1.9.0', contract: '1.40', os: 'darwin', arch: 'arm64', log: ['t ERROR load failed'] } as never)
    vi.mocked(api.getSettings).mockResolvedValue({ problem_reports: true } as never)
    vi.mocked(api.sendReport).mockReset()
  })

  it('shows everything a report sends, and sends it to the Toskar team', async () => {
    vi.mocked(api.sendReport).mockResolvedValue({ status: 'created', number: 4 })
    open()
    fireEvent.change(screen.getByLabelText('What were you doing? (optional)'), { target: { value: 'asked a question' } })
    fireEvent.change(screen.getByLabelText(/Email, for a reply/), { target: { value: 'me@example.com' } })
    expect(await screen.findByText(/Toskar 1\.9\.0, contract 1\.40/)).toHaveTextContent('Error: model failed for <email>')
    const dialog = screen.getByRole('dialog')
    expect(dialog).toHaveTextContent('asked a question')
    expect(dialog).toHaveTextContent('t ERROR load failed')
    // The reply address is the team's alone, never in what's sent along.
    expect(screen.getByText(/Toskar 1\.9\.0/)).not.toHaveTextContent('me@example.com')
    expect(screen.getByRole('link', { name: 'Or send it by email' }).getAttribute('href')).toMatch(/^mailto:info@yeix\.io\?subject=Toskar%20problem%3A%20MODEL_LOAD_FAILED&body=/)
    fireEvent.click(screen.getByRole('button', { name: 'Send' }))
    expect(await screen.findByText('Sent. Thanks: the Toskar team will look at it.')).toBeInTheDocument()
    expect(api.sendReport).toHaveBeenCalledWith({
      product: 'core',
      app: 'web 1.9.0',
      where: '/chat',
      code: 'MODEL_LOAD_FAILED',
      error: 'model failed for mike@example.com',
      note: 'asked a question',
      email: 'me@example.com',
      details: true,
    })
  })

  it('says when others hit it too, and leaves details out when asked', async () => {
    vi.mocked(api.sendReport).mockResolvedValue({ status: 'added', number: 4 })
    open()
    await screen.findByText(/Toskar 1\.9\.0/)
    fireEvent.click(screen.getByLabelText(/Include details/))
    expect(screen.queryByText(/Toskar 1\.9\.0/)).not.toBeInTheDocument()
    expect(screen.getByRole('dialog')).not.toHaveTextContent('t ERROR load failed')
    fireEvent.click(screen.getByRole('button', { name: 'Send' }))
    expect(await screen.findByText(/Others hit this too/)).toBeInTheDocument()
    expect(vi.mocked(api.sendReport).mock.calls[0][0].details).toBe(false)
  })

  it('offers the same report by email when it can’t be sent from here', async () => {
    vi.mocked(api.sendReport).mockRejectedValue(failure('REPORTS_UNAVAILABLE'))
    open()
    await screen.findByText(/Toskar 1\.9\.0/)
    fireEvent.click(screen.getByRole('button', { name: 'Send' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Send the same report by email instead.')
    expect(screen.getByRole('link', { name: 'Send by email' }).getAttribute('href')).toMatch(/^mailto:info@yeix\.io/)
  })

  it('only copies the report when an Admin turned sending off', async () => {
    vi.mocked(api.getSettings).mockResolvedValue({ problem_reports: false } as never)
    open()
    expect(await screen.findByText(/An Admin turned off sending reports to Toskar/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Send' })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: /email/i })).not.toBeInTheDocument()
    expect(screen.queryByLabelText(/Email, for a reply/)).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Copy' })).toBeInTheDocument()
  })

  it('asks to fix the address when the computer says it isn’t one', async () => {
    vi.mocked(api.sendReport).mockRejectedValue(failure('REPORT_INVALID'))
    open()
    await screen.findByText(/Toskar 1\.9\.0/)
    fireEvent.click(screen.getByRole('button', { name: 'Send' }))
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('Check the email address'))
    expect(screen.getByRole('button', { name: 'Send' })).toBeInTheDocument()
  })
})
