import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/lib/api'
import { notifyDesktopBackgroundMode } from '@/lib/desktopBridge'
import { ScheduleBackgroundSync } from './ScheduleBackgroundSync'

vi.mock('@/lib/api', () => ({
  api: {
    getSettings: vi.fn(),
    updateSettings: vi.fn(),
  },
}))

vi.mock('@/lib/desktopBridge', () => ({
  notifyDesktopBackgroundMode: vi.fn(),
}))

function renderSync() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <ScheduleBackgroundSync />
    </QueryClientProvider>,
  )
}

describe('ScheduleBackgroundSync', () => {
  beforeEach(() => {
    vi.mocked(api.getSettings).mockReset()
    vi.mocked(api.updateSettings).mockReset()
    vi.mocked(notifyDesktopBackgroundMode).mockReset()
  })

  it('tells the desktop app the saved setting', async () => {
    vi.mocked(api.getSettings).mockResolvedValue({ keep_running_in_background: true } as never)
    renderSync()
    await waitFor(() => expect(notifyDesktopBackgroundMode).toHaveBeenCalledWith(true))
  })

  it("keeps the person's choice to turn it off (#1209)", async () => {
    vi.mocked(api.getSettings).mockResolvedValue({ keep_running_in_background: false } as never)
    renderSync()
    await waitFor(() => expect(notifyDesktopBackgroundMode).toHaveBeenCalledWith(false))
    expect(api.updateSettings).not.toHaveBeenCalled()
  })
})
