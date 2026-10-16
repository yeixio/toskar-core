import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import { api } from '@/lib/api'
import { SettingsPage } from './SettingsPage'

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  const refused = () => Promise.reject(new actual.ApiError(403, 'this needs the admin role', 'ROLE_REQUIRED'))
  return {
    ...actual,
    api: new Proxy({ getMe: vi.fn(), signOut: vi.fn() } as Record<string, unknown>, {
      get: (target, key: string) => target[key] ?? refused,
    }),
  }
})

describe('SettingsPage for a Member (#203, #206)', () => {
  it('shows their account, appearance, and languages, and says the rest is for Admins', async () => {
    vi.mocked(api.getMe).mockResolvedValue({
      person: { id: 'sam', name: 'Sam', username: 'sam', role: 'member', created_at: '', sign_in: true },
      via: 'session',
    })
    render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <MemoryRouter initialEntries={['/settings']}>
          <SettingsPage />
        </MemoryRouter>
      </QueryClientProvider>,
    )
    expect(await screen.findByText(/so they're for Admins and the Owner/)).toBeInTheDocument()
    expect(screen.getByText('Signed in as Sam (@sam), Member.')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Appearance' })).toBeInTheDocument()
    // Their own languages and style are theirs to set (#206).
    expect(screen.getByRole('heading', { name: 'Language' })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Default profile' })).not.toBeInTheDocument()
  })
})

describe('SettingsPage when its settings can’t be read (#217)', () => {
  it('says so with Retry, and shows no controls that would save defaults over them', async () => {
    vi.mocked(api.getMe).mockResolvedValue({
      person: { id: 'o', name: 'Olive', username: 'olive', role: 'owner', created_at: '', sign_in: true },
      via: 'session',
    })
    // Every call but getMe fails in this file, getSettings included.
    render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <MemoryRouter initialEntries={['/settings']}>
          <SettingsPage />
        </MemoryRouter>
      </QueryClientProvider>,
    )
    expect(await screen.findByRole('heading', { name: "Toskar couldn't load this" })).toBeInTheDocument()
    expect(screen.getAllByRole('button', { name: 'Try again' }).length).toBeGreaterThan(0)
    expect(screen.queryByRole('heading', { name: 'Default profile' })).not.toBeInTheDocument()
    expect(screen.queryByText(/so they're for Admins and the Owner/)).not.toBeInTheDocument()
  })
})
