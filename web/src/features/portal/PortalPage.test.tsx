import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api, ApiError, setPortal, setPortalSession, streamChat } from '@/lib/api'
import type { PortalPageView } from '@/types/api'
import { LINK_CHECK_WAIT_MS, PortalPage } from './PortalPage'

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ApiError: actual.ApiError,
    errorText: actual.errorText,
    setPortal: vi.fn(),
    setPortalSession: vi.fn(),
    streamChat: vi.fn(),
    api: { getPortalPage: vi.fn(), enterPortal: vi.fn(), getMessages: vi.fn(), createConversation: vi.fn(), stopChat: vi.fn(), getConversations: vi.fn() },
  }
})

const guest = { person: { id: 'g1', name: 'Guest', role: 'visitor' as const, created_at: '', sign_in: false }, via: 'session' }

function page(over: Partial<PortalPageView> = {}): PortalPageView {
  return {
    slug: 'support',
    name: 'Help desk',
    access: 'passcode',
    language: 'en',
    entered: false,
    max_message: 2000,
    branding: { welcome: 'Hi! Ask about your order.', prompts: ['Where is my order?'], footer: 'Run by the Juneau shop', accent: '#0a7d6f' },
    ...over,
  }
}

function renderIt() {
  return render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <PortalPage slug="support" />
    </QueryClientProvider>,
  )
}

describe('PortalPage (#205)', () => {
  beforeEach(() => {
    vi.mocked(setPortal).mockClear()
    vi.mocked(api.enterPortal).mockReset()
    localStorage.clear()
  })

  it('asks for the passcode, then chats as the portal’s guest', async () => {
    vi.mocked(api.getPortalPage).mockResolvedValue(page())
    vi.mocked(api.enterPortal).mockRejectedValueOnce(new ApiError(401, 'wrong', 'PORTAL_PASSCODE')).mockResolvedValueOnce(guest)
    vi.mocked(api.createConversation).mockResolvedValue({ id: 'c1' } as never)
    vi.mocked(api.getMessages).mockResolvedValue([])
    // The reply streams after a moment, as the service's does, while the
    // new chat's id is known.
    vi.mocked(streamChat).mockImplementation(async ({ onToken }) => {
      await new Promise((resolve) => setTimeout(resolve, 20))
      onToken('It ships ')
      onToken('tomorrow.')
    })
    renderIt()
    expect(setPortal).toHaveBeenCalledWith('support')
    expect(await screen.findByRole('heading', { name: 'Help desk' })).toBeInTheDocument()
    expect(screen.getByText('Run by the Juneau shop')).toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('Passcode'), { target: { value: 'nope' } })
    fireEvent.click(screen.getByRole('button', { name: 'Start chatting' }))
    expect(await screen.findByText("That passcode isn't right.")).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('Passcode'), { target: { value: 'tide-pool' } })
    fireEvent.click(screen.getByRole('button', { name: 'Start chatting' }))
    await waitFor(() => expect(api.enterPortal).toHaveBeenLastCalledWith('support', 'tide-pool', '', false))

    expect(await screen.findByText('Hi! Ask about your order.')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Where is my order?' }))
    expect(await screen.findByText('It ships tomorrow.')).toBeInTheDocument()
    expect(streamChat).toHaveBeenCalledWith(expect.objectContaining({ body: { conversation_id: 'c1', message: 'Where is my order?' } }))
    expect(localStorage.getItem('toskar.portal.support.conversation')).toBe('c1')
  })

  describe('link marks (#209)', () => {
    afterEach(() => {
      vi.useRealTimers()
    })

    it('marks a link that didn’t open, read again once the check is done', async () => {
      vi.useFakeTimers({ shouldAdvanceTime: true })
      vi.mocked(api.getPortalPage).mockResolvedValue(page({ access: 'open' }))
      vi.mocked(api.enterPortal).mockResolvedValue(guest)
      vi.mocked(api.createConversation).mockResolvedValue({ id: 'c1' } as never)
      const answer = 'Track it at [the tracker](https://ship.example/track).'
      vi.mocked(api.getMessages).mockReset().mockResolvedValue([
        { id: 'm1', conversation_id: 'c1', role: 'user', content: 'Where is my order?', created_at: '' },
        { id: 'm2', conversation_id: 'c1', role: 'assistant', content: answer, created_at: '', meta: { link_check: { dead: ['https://ship.example/track'] } } },
      ] as never)
      vi.mocked(streamChat).mockImplementation(async ({ onToken }) => {
        onToken(answer)
      })
      renderIt()
      fireEvent.click(await screen.findByRole('button', { name: 'Where is my order?' }))
      expect(await screen.findByRole('link', { name: 'the tracker' })).toBeInTheDocument()
      expect(screen.queryByRole('img', { name: "This link didn't open when checked" })).not.toBeInTheDocument()
      expect(api.getMessages).not.toHaveBeenCalled()

      await vi.advanceTimersByTimeAsync(LINK_CHECK_WAIT_MS)
      expect(await screen.findByRole('img', { name: "This link didn't open when checked" })).toHaveAttribute('data-link-status', 'dead')
      expect(api.getMessages).toHaveBeenCalledTimes(1)
    })

    it('doesn’t read the chat again for an answer without links', async () => {
      vi.useFakeTimers({ shouldAdvanceTime: true })
      vi.mocked(api.getPortalPage).mockResolvedValue(page({ access: 'open' }))
      vi.mocked(api.enterPortal).mockResolvedValue(guest)
      vi.mocked(api.createConversation).mockResolvedValue({ id: 'c1' } as never)
      vi.mocked(api.getMessages).mockReset().mockResolvedValue([])
      vi.mocked(streamChat).mockImplementation(async ({ onToken }) => {
        onToken('It ships tomorrow.')
      })
      renderIt()
      fireEvent.click(await screen.findByRole('button', { name: 'Where is my order?' }))
      expect(await screen.findByText('It ships tomorrow.')).toBeInTheDocument()
      await vi.advanceTimersByTimeAsync(LINK_CHECK_WAIT_MS)
      expect(api.getMessages).not.toHaveBeenCalled()
    })
  })

  it('lets anyone into an open portal at once', async () => {
    vi.mocked(api.getPortalPage).mockResolvedValue(page({ access: 'open' }))
    vi.mocked(api.enterPortal).mockResolvedValue(guest)
    renderIt()
    expect(await screen.findByText('Hi! Ask about your order.')).toBeInTheDocument()
    expect(api.enterPortal).toHaveBeenCalledWith('support', '', '', false)
    expect(screen.queryByLabelText('Passcode')).not.toBeInTheDocument()
    // Visitors are told their chats can be read.
    expect(screen.getByText('The people who run Help desk can read these chats.')).toBeInTheDocument()
  })

  it('says when there’s no portal, or it’s off', async () => {
    vi.mocked(api.getPortalPage).mockResolvedValue(null)
    renderIt()
    expect(await screen.findByRole('heading', { name: "This chat isn't available" })).toBeInTheDocument()
  })

  it('says which limit stopped a message', async () => {
    vi.mocked(api.getPortalPage).mockResolvedValue(page({ access: 'open', entered: true }))
    vi.mocked(api.createConversation).mockResolvedValue({ id: 'c2' } as never)
    vi.mocked(streamChat).mockImplementation(async ({ onError }) => {
      onError?.("you've sent a lot of messages", 'PORTAL_RATE')
    })
    renderIt()
    fireEvent.click(await screen.findByRole('button', { name: 'Where is my order?' }))
    expect(await screen.findByRole('alert')).toHaveTextContent("You've sent a lot of messages. Try again later.")
    expect(screen.queryByText('Thinking…')).not.toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: 'Message Help desk' })).toHaveAttribute('maxlength', '2000')
  })

  it('enters with an invitation link, and takes the token out of the address', async () => {
    window.history.pushState({}, '', '/p/support?invite=tok-1')
    vi.mocked(api.getPortalPage).mockResolvedValue(page({ access: 'invited' }))
    vi.mocked(api.enterPortal).mockResolvedValue(guest)
    renderIt()
    expect(await screen.findByText('Hi! Ask about your order.')).toBeInTheDocument()
    expect(api.enterPortal).toHaveBeenCalledWith('support', '', 'tok-1', false)
    expect(window.location.search).toBe('')
    window.history.pushState({}, '', '/')
  })

  it('says an invited portal needs its link, and a Members one a sign-in', async () => {
    vi.mocked(api.getPortalPage).mockResolvedValue(page({ access: 'invited' }))
    const { unmount } = renderIt()
    expect(await screen.findByText('Help desk is by invitation. Open the link you were sent.')).toBeInTheDocument()
    unmount()
    vi.mocked(api.getPortalPage).mockResolvedValue(page({ access: 'members' }))
    renderIt()
    expect(await screen.findByRole('link', { name: 'Sign in to chat' })).toHaveAttribute('href', '/?next=%2Fp%2Fsupport')
  })

  it('keeps its session in a header when shown in another website', async () => {
    const top = Object.getOwnPropertyDescriptor(window, 'top')
    Object.defineProperty(window, 'top', { configurable: true, value: {} })
    try {
      vi.mocked(api.getPortalPage).mockResolvedValue(page({ access: 'open' }))
      vi.mocked(api.enterPortal).mockResolvedValue({ ...guest, session: 'sess-1' })
      renderIt()
      expect(await screen.findByText('Hi! Ask about your order.')).toBeInTheDocument()
      expect(api.enterPortal).toHaveBeenCalledWith('support', '', '', true)
      expect(setPortalSession).toHaveBeenCalledWith('sess-1')
      expect(sessionStorage.getItem('toskar.portal.support.session')).toBe('sess-1')
    } finally {
      if (top) Object.defineProperty(window, 'top', top)
      sessionStorage.clear()
    }
  })

  it('starts a new chat when the saved one is no longer the visitor’s', async () => {
    localStorage.setItem('toskar.portal.support.conversation', 'gone-1')
    vi.mocked(api.getPortalPage).mockResolvedValue(page({ access: 'open', entered: true }))
    vi.mocked(api.getConversations).mockResolvedValue([])
    vi.mocked(api.createConversation).mockResolvedValue({ id: 'c9' } as never)
    vi.mocked(streamChat).mockImplementation(async ({ onToken }) => onToken('Hello again.'))
    renderIt()
    fireEvent.click(await screen.findByRole('button', { name: 'Where is my order?' }))
    expect(await screen.findByText('Hello again.')).toBeInTheDocument()
    expect(api.getMessages).not.toHaveBeenCalledWith('gone-1')
    expect(streamChat).toHaveBeenLastCalledWith(expect.objectContaining({ body: { conversation_id: 'c9', message: 'Where is my order?' } }))
    expect(localStorage.getItem('toskar.portal.support.conversation')).toBe('c9')
  })
})
