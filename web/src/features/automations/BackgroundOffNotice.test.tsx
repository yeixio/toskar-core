import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/lib/api'
import { isDesktopShell } from '@/lib/desktopBridge'
import { BackgroundOffNotice } from './BackgroundOffNotice'

vi.mock('@/lib/api', () => ({ api: { getSettings: vi.fn() } }))
vi.mock('@/lib/desktopBridge', () => ({ isDesktopShell: vi.fn() }))

function renderNotice() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <BackgroundOffNotice />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('BackgroundOffNotice (#1209)', () => {
  beforeEach(() => {
    vi.mocked(isDesktopShell).mockReturnValue(true)
  })

  it('says schedules stop with the window when background mode is off', async () => {
    vi.mocked(api.getSettings).mockResolvedValue({ keep_running_in_background: false } as never)
    renderNotice()
    expect(await screen.findByRole('note')).toHaveTextContent(/scheduled automations won't run/i)
    expect(screen.getByRole('link', { name: /keep running in background/i })).toHaveAttribute('href', '/settings/general')
  })

  it('says nothing when it is on', async () => {
    vi.mocked(api.getSettings).mockResolvedValue({ keep_running_in_background: true } as never)
    renderNotice()
    await new Promise((r) => setTimeout(r, 20))
    expect(screen.queryByRole('note')).toBeNull()
  })
})
