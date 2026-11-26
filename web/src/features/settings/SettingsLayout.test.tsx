import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import { MovedPage } from '@/app/MovedPage'
import { api } from '@/lib/api'
import { SettingsLayout } from './SettingsLayout'

vi.mock('@/lib/api', () => ({
  api: {
    getMe: vi.fn(async () => {
      throw new Error('no /me')
    }),
  },
}))

function Where() {
  const { pathname, search, hash } = useLocation()
  return <p>at {pathname + search + hash}</p>
}

function renderAt(path: string) {
  return render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/settings" element={<SettingsLayout />}>
            <Route index element={<h1>Account</h1>} />
            <Route path="*" element={<Where />} />
          </Route>
          <Route path="/nodes" element={<MovedPage to="/settings/computers" />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

const settingsLinks = () =>
  within(screen.getByRole('navigation', { name: 'Settings' }))
    .getAllByRole('link')
    .map((a) => a.getAttribute('href'))

describe('SettingsLayout (#1374, #1321)', () => {
  it('lists your own settings first, then the computer and team', () => {
    renderAt('/settings')
    expect(screen.getByText('You')).toBeInTheDocument()
    expect(screen.getByText('This computer and team')).toBeInTheDocument()
    expect(settingsLinks()).toEqual([
      '/settings/account',
      '/settings/general',
      '/settings/appearance',
      '/settings/personalization',
      '/settings/memory',
      '/settings/notifications',
      '/settings/voice',
      '/settings/features',
      '/settings/models',
      '/settings/history',
      '/settings/computers',
      '/settings/network',
      '/settings/people',
      '/settings/portals',
      '/settings/tools',
      '/settings/apps',
      '/settings/privacy',
      '/settings/advanced',
      '/settings/performance',
      '/settings/diagnostics',
      '/settings/developer',
      '/settings/about',
    ])
  })

  it('finds a page by the name of a setting on it', () => {
    renderAt('/settings')
    fireEvent.change(screen.getByRole('searchbox', { name: 'Search settings' }), { target: { value: 'lifecycle' } })
    expect(settingsLinks()).toEqual(['/settings/models'])
    fireEvent.change(screen.getByRole('searchbox', { name: 'Search settings' }), { target: { value: 'zzz' } })
    expect(screen.getByText('No setting matches that.')).toBeInTheDocument()
  })

  it('shows a Member only their own pages', async () => {
    vi.mocked(api.getMe).mockResolvedValueOnce({
      person: { id: 'sam', name: 'Sam', created_at: '', sign_in: true, role: 'member' },
      via: 'session',
    } as never)
    renderAt('/settings')
    await waitFor(() =>
      expect(settingsLinks()).toEqual([
        '/settings/account',
        '/settings/general',
        '/settings/appearance',
        '/settings/personalization',
        '/settings/memory',
        '/settings/notifications',
        '/settings/voice',
      ]),
    )
    expect(screen.queryByText('This computer and team')).not.toBeInTheDocument()
  })

  it('opens a moved page at its new place, with its query and anchor', async () => {
    renderAt('/nodes?pair=1#nearby')
    expect(await screen.findByText('at /settings/computers?pair=1#nearby')).toBeInTheDocument()
  })
})
