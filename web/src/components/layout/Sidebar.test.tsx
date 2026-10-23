import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/lib/api'
import { useUIStore } from '@/stores/uiStore'
import { Sidebar } from './Sidebar'

vi.mock('@/lib/api', () => ({
  api: {
    getHealth: vi.fn(async () => ({ status: 'ok', product: 'yggdrasil', version: 'test' })),
    getNodes: vi.fn(async () => []),
    getModels: vi.fn(async () => []),
    getMe: vi.fn(async () => {
      throw new Error('no /me')
    }),
    listNotifications: vi.fn(async () => []),
    markNotificationsRead: vi.fn(),
    dismissNotification: vi.fn(),
  },
}))

function renderAt(path: string) {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <MemoryRouter initialEntries={[path]}>
        <Sidebar />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

const links = () => within(screen.getByRole('navigation')).getAllByRole('link').map((a) => a.getAttribute('href'))

describe('Sidebar', () => {
  beforeEach(() => {
    useUIStore.setState({ advancedMode: false, administerOpen: false })
  })

  it('groups pages into Use, Customize, and Administer, with Settings last, and lists Tools and Profiles without advanced mode', () => {
    renderAt('/chat')
    expect(screen.getByText('Use')).toBeInTheDocument()
    expect(screen.getByText('Customize')).toBeInTheDocument()
    expect(links()).toEqual(['/chat', '/automations', '/knowledge', '/memory', '/news', '/models', '/tools', '/profiles', '/settings'])
  })

  it('keeps Administer closed until it is opened, and remembers the choice', () => {
    renderAt('/chat')
    const toggle = screen.getByRole('button', { name: 'Administer' })
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByRole('link', { name: /Computers/ })).not.toBeInTheDocument()

    fireEvent.click(toggle)
    expect(toggle).toHaveAttribute('aria-expanded', 'true')
    expect(links()).toEqual([
      '/chat', '/automations', '/knowledge', '/memory', '/news', '/models', '/tools', '/profiles',
      '/train', '/nodes', '/people', '/portals', '/api-access', '/performance', '/diagnostics', '/settings',
    ])
    expect(useUIStore.getState().administerOpen).toBe(true)

    fireEvent.click(toggle)
    expect(useUIStore.getState().administerOpen).toBe(false)
    expect(screen.queryByRole('link', { name: /Diagnostics/ })).not.toBeInTheDocument()
  })

  it('shows Administer while one of its pages is open, so the current page is in view', () => {
    renderAt('/nodes')
    const toggle = screen.getByRole('button', { name: 'Administer' })
    expect(toggle).toHaveAttribute('aria-expanded', 'true')
    expect(screen.getByRole('link', { name: /Computers/ })).toHaveClass('nav-link-active')
  })

  it('shows Members what they use and Visitors only Chat (#203)', async () => {
    const person = { id: 'sam', name: 'Sam', created_at: '', sign_in: true }
    vi.mocked(api.getMe).mockResolvedValueOnce({ person: { ...person, role: 'member' }, via: 'session' })
    const { unmount } = renderAt('/chat')
    await waitFor(() => expect(links()).toEqual(['/chat', '/automations', '/memory', '/news', '/settings']))
    expect(screen.queryByRole('button', { name: 'Administer' })).not.toBeInTheDocument()
    expect(screen.queryByText('Customize')).not.toBeInTheDocument()
    unmount()

    vi.mocked(api.getMe).mockResolvedValueOnce({ person: { ...person, role: 'visitor' }, via: 'session' })
    renderAt('/chat')
    await waitFor(() => expect(links()).toEqual(['/chat', '/settings']))
  })
})
