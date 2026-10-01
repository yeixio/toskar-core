import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/lib/api'
import { WhatLeft } from './WhatLeft'

vi.mock('@/lib/api', () => ({
  api: { getPrivacy: vi.fn(), listEgress: vi.fn(), setRunRetention: vi.fn(), deleteRunRecords: vi.fn() },
}))

function renderIt() {
  return render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <WhatLeft />
    </QueryClientProvider>,
  )
}

describe('WhatLeft', () => {
  beforeEach(() => {
    vi.mocked(api.getPrivacy).mockResolvedValue({ retention_days: 30, last_30_days: { web_search: 2, connector: 1 } })
    vi.mocked(api.listEgress).mockResolvedValue([
      { id: '1', at: '2026-10-01T20:00:00Z', kind: 'web_search', destination: 'DuckDuckGo', detail: 'tide times', source: 'chat' },
      { id: '2', at: '2026-10-01T20:01:00Z', kind: 'connector', destination: 'GitHub', detail: 'Search GitHub (query: is:open)', source: 'api' },
    ])
    vi.mocked(api.setRunRetention).mockResolvedValue({ retention_days: 7, last_30_days: {} })
    vi.mocked(api.deleteRunRecords).mockResolvedValue({ tasks: 4, automation_runs: 1, egress: 2 })
  })

  it('lists what left, changes retention, and deletes run records', async () => {
    renderIt()
    expect(await screen.findByText('In the last 30 days: web search 2, connected service 1.')).toBeInTheDocument()
    expect(screen.getByText('tide times')).toBeInTheDocument()
    expect(screen.getByText('GitHub')).toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('Keep run records for'), { target: { value: '7' } })
    await waitFor(() => expect(api.setRunRetention).toHaveBeenCalledWith(7))

    vi.spyOn(window, 'confirm').mockReturnValue(true)
    fireEvent.click(screen.getByRole('button', { name: 'Delete run records now' }))
    expect(await screen.findByText(/Deleted 4 run records, 1 automation results, and 2 entries/)).toBeInTheDocument()
  })
})
