import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import type { ReactElement } from 'react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { api, forgetApiKey } from '@/lib/api'
import { useUIStore } from '@/stores/uiStore'
import { InvitePage } from './InvitePage'
import { SignInForm } from './SignInForm'

vi.mock('@/lib/api', () => ({
  api: { peekInvite: vi.fn(), acceptInvite: vi.fn(), signIn: vi.fn(), getOIDC: vi.fn() },
  forgetApiKey: vi.fn(),
  getApiBase: () => '',
}))

function renderInvite() {
  return render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <MemoryRouter initialEntries={['/invite/tok123']}>
        <Routes>
          <Route path="/invite/:token" element={<InvitePage />} />
          <Route path="/chat" element={<p>chat page</p>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('InvitePage', () => {
  beforeEach(() => {
    vi.mocked(forgetApiKey).mockClear()
    useUIStore.getState().resetToDefaults()
  })

  it('chooses a username and password, then opens chat signed in', async () => {
    vi.mocked(api.peekInvite).mockResolvedValue({ name: 'Robin', kind: 'invite' })
    vi.mocked(api.acceptInvite).mockResolvedValue({ person: { id: 'p', name: 'Robin', role: 'member', created_at: '', sign_in: true }, via: 'session' })
    renderInvite()
    expect(await screen.findByRole('heading', { name: 'Welcome, Robin' })).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText(/^Username/), { target: { value: ' robin ' } })
    fireEvent.change(screen.getByLabelText(/^Password\s*At least/), { target: { value: 'long enough pw' } })
    fireEvent.change(screen.getByLabelText('Password again'), { target: { value: 'not the same' } })
    fireEvent.click(screen.getByRole('button', { name: 'Sign in' }))
    expect(await screen.findByText("The passwords don't match.")).toBeInTheDocument()
    expect(api.acceptInvite).not.toHaveBeenCalled()

    fireEvent.change(screen.getByLabelText('Password again'), { target: { value: 'long enough pw' } })
    fireEvent.click(screen.getByRole('button', { name: 'Sign in' }))
    expect(await screen.findByText('chat page')).toBeInTheDocument()
    expect(api.acceptInvite).toHaveBeenCalledWith('tok123', 'robin', 'long enough pw')
    expect(forgetApiKey).toHaveBeenCalled()
    expect(useUIStore.getState().onboardingComplete).toBe(true)
  })

  it('sets a new password for the username already chosen', async () => {
    vi.mocked(api.peekInvite).mockResolvedValue({ name: 'Grace', kind: 'reset', username: 'grace' })
    vi.mocked(api.acceptInvite).mockResolvedValue({ person: { id: 'p', name: 'Grace', role: 'admin', created_at: '', sign_in: true }, via: 'session' })
    renderInvite()
    expect(await screen.findByText('@grace')).toBeInTheDocument()
    expect(screen.queryByLabelText(/^Username/)).not.toBeInTheDocument()
    fireEvent.change(screen.getByLabelText(/^Password\s*At least/), { target: { value: 'another pw 12' } })
    fireEvent.change(screen.getByLabelText('Password again'), { target: { value: 'another pw 12' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save and sign in' }))
    await waitFor(() => expect(api.acceptInvite).toHaveBeenCalledWith('tok123', 'grace', 'another pw 12'))
  })

  it('says when a link no longer works', async () => {
    // Toskar answers 404 for a link that's used up or expired.
    vi.mocked(api.peekInvite).mockResolvedValue(null)
    renderInvite()
    expect(await screen.findByRole('heading', { name: "This link doesn't work anymore" })).toBeInTheDocument()
  })

  it('says a link it couldn’t check couldn’t be checked, with Retry (#217)', async () => {
    vi.mocked(api.peekInvite).mockRejectedValue(Object.assign(new Error('Failed to fetch'), { status: 0 }))
    renderInvite()
    expect(await screen.findByRole('button', { name: 'Try again' })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: "This link doesn't work anymore" })).not.toBeInTheDocument()
  })
})

function renderSignIn(ui: ReactElement) {
  return render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>{ui}</QueryClientProvider>)
}

describe('SignInForm', () => {
  it('offers the provider, and says why it sent someone back (#206)', async () => {
    vi.mocked(api.getOIDC).mockResolvedValue({ enabled: true, label: 'Authentik' })
    window.history.pushState({}, '', '/chat?oidc_error=refused')
    renderSignIn(<SignInForm onSignedIn={vi.fn()} onUseKey={vi.fn()} />)
    const link = await screen.findByRole('link', { name: 'Sign in with Authentik' })
    expect(link).toHaveAttribute('href', '/api/v1/oidc/start?return=%2Fchat')
    expect(screen.getByRole('alert')).toHaveTextContent("Your account at Authentik doesn't give you a role")
    window.history.pushState({}, '', '/')
  })

  it('goes back to a chat portal a Member signs in for (#205)', async () => {
    vi.mocked(api.getOIDC).mockResolvedValue({ enabled: true, label: 'Authentik' })
    window.history.pushState({}, '', '/?next=%2Fp%2Fsupport')
    renderSignIn(<SignInForm onSignedIn={vi.fn()} onUseKey={vi.fn()} />)
    expect(await screen.findByRole('link', { name: 'Sign in with Authentik' })).toHaveAttribute('href', '/api/v1/oidc/start?return=%2Fp%2Fsupport')
    window.history.pushState({}, '', '/')
  })

  it('signs in, or offers an API key instead', async () => {
    const onSignedIn = vi.fn()
    const onUseKey = vi.fn()
    vi.mocked(api.signIn).mockRejectedValueOnce(new Error('That username and password don’t match.'))
    vi.mocked(api.signIn).mockResolvedValueOnce({ person: { id: 'p', name: 'Grace', role: 'admin', created_at: '', sign_in: true }, via: 'session' })
    vi.mocked(api.getOIDC).mockResolvedValue({ enabled: false })
    renderSignIn(<SignInForm onSignedIn={onSignedIn} onUseKey={onUseKey} />)
    fireEvent.change(screen.getByLabelText('Username'), { target: { value: 'grace' } })
    fireEvent.change(screen.getByLabelText('Password'), { target: { value: 'wrong' } })
    fireEvent.click(screen.getByRole('button', { name: 'Sign in' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('don’t match')
    fireEvent.click(screen.getByRole('button', { name: 'Sign in' }))
    await waitFor(() => expect(onSignedIn).toHaveBeenCalled())
    expect(api.signIn).toHaveBeenLastCalledWith('grace', 'wrong')
    fireEvent.click(screen.getByRole('button', { name: 'Use an API key instead' }))
    expect(onUseKey).toHaveBeenCalled()
  })
})
