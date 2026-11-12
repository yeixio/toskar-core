import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { api, streamChat } from '@/lib/api'
import { useUIStore } from '@/stores/uiStore'
import type { Conversation, Project } from '@/types/api'
import { ChatPage } from './ChatPage'

// Chat with projects (#1136): a new chat started from a project's page is
// in it, and the line under a chat's header moves it or takes it out.
vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    streamChat: vi.fn(async () => undefined),
    api: {
      ...actual.api,
      getConversations: vi.fn(),
      listProjects: vi.fn(),
      createConversation: vi.fn(),
      updateConversation: vi.fn(),
      getMessages: vi.fn(async () => []),
      getModels: vi.fn(async () => [{ id: 'm1', display_name: 'Model one', installed: true }]),
    },
  }
})

const now = new Date().toISOString()

const kitchen: Project = {
  id: 'kitchen',
  name: 'Kitchen remodel',
  instructions: '',
  folder: '/work/kitchen',
  memory_off: false,
  knowledge: [],
  files: [],
  chat_count: 1,
  created_at: now,
  updated_at: now,
}
const garden: Project = { ...kitchen, id: 'garden', name: 'Garden', folder: '' }
const quotes: Conversation = { id: 'c1', title: 'Cabinet quotes', project_id: 'kitchen', created_at: now, updated_at: now }

function renderChat(entry: string) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[entry]}>
        <ChatPage />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('Chat in a project (#1136)', () => {
  beforeEach(() => {
    useUIStore.getState().resetToDefaults()
    // Everything else the page asks for has no answer.
    vi.stubGlobal('fetch', vi.fn(async () => new Response(null, { status: 404 })))
    vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} }))
    vi.stubGlobal(
      'ResizeObserver',
      class {
        observe() {}
        unobserve() {}
        disconnect() {}
      },
    )
    vi.mocked(api.getConversations).mockResolvedValue([quotes])
    vi.mocked(api.listProjects).mockResolvedValue([kitchen, garden])
    vi.mocked(api.createConversation).mockResolvedValue({ id: 'c-new', title: 'Measure', project_id: 'kitchen', created_at: now, updated_at: now })
    vi.mocked(api.updateConversation).mockResolvedValue(quotes)
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.clearAllMocks()
  })

  it('starts a new chat from a project page in the project', async () => {
    renderChat('/chat?new=1&project=kitchen')
    expect(await screen.findByText('Kitchen remodel')).toBeInTheDocument()
    expect(screen.getByText(/New chat in project/)).toBeInTheDocument()
    fireEvent.change(screen.getByRole('textbox', { name: 'Message' }), { target: { value: 'Measure the wall' } })
    fireEvent.submit(screen.getByRole('textbox', { name: 'Message' }).closest('form')!)
    await waitFor(() => expect(api.createConversation).toHaveBeenCalledWith(expect.objectContaining({ project_id: 'kitchen' })))
    await waitFor(() => expect(streamChat).toHaveBeenCalled())
  })

  it('says which project a chat is in, and takes it out', async () => {
    renderChat('/chat?c=c1')
    expect(await screen.findByRole('link', { name: 'Kitchen remodel' })).toHaveAttribute('href', '/projects/kitchen')
    fireEvent.click(screen.getByRole('button', { name: 'Remove from project' }))
    await waitFor(() => expect(api.updateConversation).toHaveBeenCalledWith('c1', { project_id: '' }))
  })

  it('moves a chat to another project', async () => {
    renderChat('/chat?c=c1')
    fireEvent.click(await screen.findByRole('button', { name: 'Move to project' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Garden' }))
    await waitFor(() => expect(api.updateConversation).toHaveBeenCalledWith('c1', { project_id: 'garden' }))
  })

  it("doesn't start a chat in a project when New chat is used", async () => {
    renderChat('/chat?new=1&project=kitchen')
    expect(await screen.findByText(/New chat in project/)).toBeInTheDocument()
    fireEvent.click(screen.getByTitle('New chat'))
    expect(screen.queryByText(/New chat in project/)).toBeNull()
  })
})
