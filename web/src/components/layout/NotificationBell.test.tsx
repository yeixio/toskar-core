import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/lib/api'
import type { AppNotification } from '@/types/api'
import { NotificationBell } from './NotificationBell'

vi.mock('@/lib/api', () => ({
  api: {
    listNotifications: vi.fn(),
    markNotificationsRead: vi.fn(),
    dismissNotification: vi.fn(),
  },
}))

const skipped: AppNotification = {
  id: 'n1',
  created_at: new Date().toISOString(),
  source_type: 'automation',
  source_id: 'a1',
  category: 'approval',
  severity: 'warning',
  title: 'Morning brief skipped files.write',
  body: 'Approve it in the automation to let it run.',
  link: '/automations?id=a1',
}

const ready: AppNotification = {
  ...skipped,
  id: 'n2',
  category: 'model',
  severity: 'success',
  title: 'Model ready',
  body: 'Qwen finished downloading.',
  link: '/models',
  read_at: new Date().toISOString(),
}

function Where() {
  const location = useLocation()
  return <p data-testid="where">{location.pathname + location.search}</p>
}

function renderBell() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={['/chat']}>
        <NotificationBell />
        <Routes>
          <Route path="*" element={<Where />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('NotificationBell', () => {
  beforeEach(() => {
    // A small fake daemon, so refetches after a change see it.
    let list = [skipped, ready]
    const now = new Date().toISOString()
    vi.mocked(api.listNotifications)
      .mockReset()
      .mockImplementation(async () => ({ notifications: list, unread: list.filter((n) => !n.read_at).length }))
    vi.mocked(api.markNotificationsRead)
      .mockReset()
      .mockImplementation(async (ids = []) => {
        list = list.map((n) => (ids.length === 0 || ids.includes(n.id) ? { ...n, read_at: n.read_at ?? now } : n))
        return null
      })
    vi.mocked(api.dismissNotification)
      .mockReset()
      .mockImplementation(async (id) => {
        list = list.filter((n) => n.id !== id)
        return null
      })
  })

  it('shows the unread count and lists notifications', async () => {
    renderBell()
    const bell = await screen.findByRole('button', { name: 'Notifications, 1 unread' })
    fireEvent.click(bell)
    expect(screen.getByRole('dialog', { name: 'Notifications' })).toBeInTheDocument()
    expect(screen.getByText('Morning brief skipped files.write')).toBeInTheDocument()
    expect(screen.getByText('Model ready')).toBeInTheDocument()
  })

  it('opening a notification marks it read and goes to its source', async () => {
    renderBell()
    fireEvent.click(await screen.findByRole('button', { name: 'Notifications, 1 unread' }))
    fireEvent.click(screen.getByText('Morning brief skipped files.write'))
    await waitFor(() => expect(api.markNotificationsRead).toHaveBeenCalledWith(['n1']))
    expect(screen.getByTestId('where')).toHaveTextContent('/automations?id=a1')
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('marks all read and dismisses', async () => {
    renderBell()
    fireEvent.click(await screen.findByRole('button', { name: 'Notifications, 1 unread' }))
    fireEvent.click(screen.getByRole('button', { name: 'Mark all read' }))
    await waitFor(() => expect(api.markNotificationsRead).toHaveBeenCalledWith([]))
    expect(await screen.findByRole('button', { name: 'Notifications' })).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Dismiss Model ready' }))
    await waitFor(() => expect(api.dismissNotification).toHaveBeenCalledWith('n2'))
    await waitFor(() => expect(screen.queryByText('Model ready')).not.toBeInTheDocument())
  })
})
