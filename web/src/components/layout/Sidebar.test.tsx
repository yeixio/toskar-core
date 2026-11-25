import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
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
    getSettings: vi.fn(async () => ({})),
    listKnowledge: vi.fn(async () => []),
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
    useUIStore.setState({ advancedMode: false })
  })

  it('lists the six places people work in, with Settings last (#1374)', () => {
    renderAt('/chat')
    expect(links()).toEqual(['/chat', '/automations', '/knowledge', '/profiles', '/models', '/news', '/settings'])
    expect(screen.queryByRole('link', { name: /Computers|Diagnostics|API Access/ })).not.toBeInTheDocument()
  })

  it('marks Settings while one of its pages is open, and Models while Train is', () => {
    const { unmount } = renderAt('/settings/computers')
    expect(screen.getByRole('link', { name: /Settings/ })).toHaveClass('nav-link-active')
    unmount()
    renderAt('/models/train')
    expect(screen.getByRole('link', { name: /Models/ })).toHaveClass('nav-link-active')
  })

  it('shows Members what they use and Visitors only Chat (#203)', async () => {
    const person = { id: 'sam', name: 'Sam', created_at: '', sign_in: true }
    vi.mocked(api.getMe).mockResolvedValueOnce({ person: { ...person, role: 'member' }, via: 'session' })
    const { unmount } = renderAt('/chat')
    await waitFor(() => expect(links()).toEqual(['/chat', '/automations', '/news', '/settings']))
    unmount()

    vi.mocked(api.getMe).mockResolvedValueOnce({ person: { ...person, role: 'visitor' }, via: 'session' })
    renderAt('/chat')
    await waitFor(() => expect(links()).toEqual(['/chat', '/settings']))
  })
})

describe('Sidebar QA regressions', () => {
  beforeEach(() => {
    useUIStore.setState({ advancedMode: false })
  })

  // Regression: toskar-core#1249
  it('a computer on its own is not "1 connected"', async () => {
    vi.mocked(api.getNodes).mockResolvedValueOnce([{ id: 'n1', name: 'qa-box', os: 'linux', is_local: true } as never])
    renderAt('/chat')
    await waitFor(() => expect(screen.queryByText('—')).not.toBeInTheDocument())
    expect(screen.queryByText('1 connected')).not.toBeInTheDocument()
  })

  // Regression: toskar-core#1249
  it('Members see no Knowledge status for a page they cannot open', async () => {
    const person = { id: 'sam', name: 'Sam', created_at: '', sign_in: true }
    vi.mocked(api.getMe).mockResolvedValueOnce({ person: { ...person, role: 'member' }, via: 'session' })
    renderAt('/chat')
    await waitFor(() => expect(links()).toEqual(['/chat', '/automations', '/news', '/settings']))
    const footer = screen.getByText('Status').closest('footer')!
    expect(within(footer).queryByText('Knowledge')).not.toBeInTheDocument()
  })

  // Regression: toskar-core#1251
  it('status tooltips use plain names, not Bifrost, Norn or Mimir', async () => {
    renderAt('/chat')
    const footer = screen.getByText('Status').closest('footer')!
    await within(footer).findByText('Knowledge')
    const titles = Array.from(footer.querySelectorAll('[title]')).map((el) => el.getAttribute('title') ?? '')
    expect(titles.filter((title) => /\b(Bifrost|Norn|Mimir|Muninn)\b/.test(title))).toEqual([])
  })

  // Regression: toskar-core#1249
  it('Placement and Knowledge say what is really set and happening', async () => {
    vi.mocked(api.getSettings).mockResolvedValueOnce({ default_execution: 'local' } as never)
    vi.mocked(api.listKnowledge).mockResolvedValueOnce([
      { id: 'k1', status: 'ready' },
      { id: 'k2', status: 'failed' },
    ] as never)
    renderAt('/chat')
    const footer = screen.getByText('Status').closest('footer')!
    await within(footer).findByText('This computer')
    await within(footer).findByText('1 failed')
    expect(within(footer).queryByText('Automatic')).not.toBeInTheDocument()
    expect(within(footer).queryByText('Ready')).not.toBeInTheDocument()
  })
})
