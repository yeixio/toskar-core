import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import { api, ApiError } from '@/lib/api'
import { ServiceBootGate } from './ServiceBootGate'

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    api: { ...actual.api, getHealth: vi.fn(), signIn: vi.fn() },
  }
})
vi.mock('./ScheduleBackgroundSync', () => ({ ScheduleBackgroundSync: () => null }))

function renderAt(path: string) {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <MemoryRouter initialEntries={[path]}>
        <ServiceBootGate>
          <p>the app</p>
        </ServiceBootGate>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('ServiceBootGate sign-in (#206)', () => {
  it('asks for a username and password, or a key, when the service wants sign-in', async () => {
    vi.mocked(api.getHealth).mockRejectedValue(new ApiError(401, 'sign in', 'UNAUTHORIZED'))
    renderAt('/chat')
    expect(await screen.findByRole('heading', { name: 'Sign in to Toskar' })).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Use an API key instead' }))
    expect(screen.getByPlaceholderText('ygg_…')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Sign in with a username instead' }))
    expect(screen.getByRole('heading', { name: 'Sign in to Toskar' })).toBeInTheDocument()

    vi.mocked(api.signIn).mockResolvedValue({ person: { id: 'p', name: 'Grace', role: 'admin', created_at: '', sign_in: true }, via: 'session' })
    vi.mocked(api.getHealth).mockResolvedValue({ status: 'ok', product: 'toskar', version: 'test' } as never)
    fireEvent.change(screen.getByLabelText('Username'), { target: { value: 'grace' } })
    fireEvent.change(screen.getByLabelText('Password'), { target: { value: 'long enough pw' } })
    fireEvent.click(screen.getByRole('button', { name: 'Sign in' }))
    expect(await screen.findByText('the app')).toBeInTheDocument()
  })

  it('opens a sign-in link before anyone is signed in', () => {
    vi.mocked(api.getHealth).mockRejectedValue(new ApiError(401, 'sign in', 'UNAUTHORIZED'))
    renderAt('/invite/abc')
    expect(screen.getByText('the app')).toBeInTheDocument()
  })

  // Regression: toskar-core#1270
  it("doesn't flash the startup screen when the service is already up", async () => {
    vi.mocked(api.getHealth).mockResolvedValue({ status: 'ok', product: 'toskar', version: 'test' } as never)
    api.getMe = vi.fn(async () => ({ person: { id: 'p', name: 'Grace', role: 'admin', created_at: '', sign_in: true }, via: 'session' })) as never
    renderAt('/api-access')
    expect(screen.queryByText('Starting Toskar')).not.toBeInTheDocument()
    expect(await screen.findByText('the app')).toBeInTheDocument()
  })

  // Regression: toskar-core#1270
  it('shows the startup screen while the service is really starting', async () => {
    vi.mocked(api.getHealth).mockReturnValue(new Promise(() => {}))
    renderAt('/chat')
    expect(await screen.findByText('Starting Toskar', {}, { timeout: 2000 })).toBeInTheDocument()
  })
})
