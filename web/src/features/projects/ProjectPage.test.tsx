import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { api } from '@/lib/api'
import type { Conversation, Principal, Project, Role, Swarm } from '@/types/api'
import { ProjectPage } from './ProjectPage'
import { NewProjectDialog } from './NewProjectDialog'
import { MoveToProjectDialog } from './MoveToProjectDialog'

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      getMe: vi.fn(),
      getProject: vi.fn(),
      listProjects: vi.fn(),
      getProjectConversations: vi.fn(),
      createProject: vi.fn(),
      updateProject: vi.fn(),
      deleteProject: vi.fn(),
      addProjectFile: vi.fn(),
      removeProjectFile: vi.fn(),
      updateConversation: vi.fn(),
      getProfiles: vi.fn(),
      getModels: vi.fn(),
      listKnowledge: vi.fn(),
      listSwarms: vi.fn(),
      getSwarmConversations: vi.fn(),
    },
  }
})

const now = new Date().toISOString()

const kitchen: Project = {
  id: 'kitchen',
  name: 'Kitchen remodel',
  instructions: 'Answers are for a small house.',
  folder: '/work/kitchen',
  memory_off: false,
  knowledge: [],
  files: [{ name: 'quotes.md', size: 2048 }],
  chat_count: 2,
  created_at: now,
  updated_at: now,
}

const chats: Conversation[] = [
  { id: 'c1', title: 'Cabinet quotes', project_id: 'kitchen', created_at: now, updated_at: now },
  { id: 'c2', title: 'Tile colors', project_id: 'kitchen', created_at: now, updated_at: now },
]

const me = (role: Role) => ({ person: { id: 'p', name: 'P', role, created_at: '', sign_in: false }, via: 'this_computer' }) as Principal

/** Where the page sent the person, shown so a test can read it. */
function Where() {
  const location = useLocation()
  return <p data-testid="where">{location.pathname + location.search}</p>
}

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={['/projects/kitchen']}>
        <Routes>
          <Route path="/projects/:id" element={<ProjectPage />} />
          <Route path="*" element={<Where />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('ProjectPage (#1136)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(api.getMe).mockResolvedValue(me('owner'))
    vi.mocked(api.getProject).mockResolvedValue(kitchen)
    vi.mocked(api.getProjectConversations).mockResolvedValue(chats)
    vi.mocked(api.updateProject).mockImplementation(async (_id, body) => ({ ...kitchen, ...body }))
    vi.mocked(api.deleteProject).mockResolvedValue(null)
    vi.mocked(api.getProfiles).mockResolvedValue([])
    vi.mocked(api.getModels).mockResolvedValue([])
    vi.mocked(api.listKnowledge).mockResolvedValue([])
    vi.mocked(api.listSwarms).mockResolvedValue([])
  })

  it("folds a swarm's agents' chats under the chat that started it (#1135)", async () => {
    const agent = (n: number) => ({ agent: n, conversation_id: `a${n}`, role: `agent:${n}`, part: '', status: 'done', tokens: 0, seconds: 0 })
    const swarm = { id: 's1', conversation_id: 'c1', project_id: 'kitchen', agents: [agent(1), agent(2)] } as unknown as Swarm
    vi.mocked(api.listSwarms).mockResolvedValue([swarm])
    vi.mocked(api.getSwarmConversations).mockResolvedValue([
      { ...chats[0], swarm_id: 's1' },
      { id: 'a1', title: 'Agent 1: Measure', project_id: 'kitchen', swarm_id: 's1', swarm_role: 'agent:1', created_at: now, updated_at: now },
      { id: 'a2', title: 'Agent 2: Price', project_id: 'kitchen', swarm_id: 's1', swarm_role: 'agent:2', created_at: now, updated_at: now },
    ])
    renderPage()
    const fold = await screen.findByRole('button', { name: /2 agents/ })
    expect(fold).toHaveAttribute('aria-expanded', 'false')
    // Only the chat that started the swarm has agents.
    expect(screen.getAllByRole('button', { name: /agents/ })).toHaveLength(1)
    expect(screen.queryByRole('link', { name: 'Agent 1: Measure' })).toBeNull()
    fireEvent.click(fold)
    expect(await screen.findByRole('link', { name: 'Agent 1: Measure' })).toHaveAttribute('href', '/chat?c=a1')
    expect(screen.getByRole('link', { name: 'Agent 2: Price' })).toHaveAttribute('href', '/chat?c=a2')
    expect(api.getSwarmConversations).toHaveBeenCalledWith('s1')
    // The coordinator's own chat isn't listed again under itself.
    expect(screen.getAllByRole('link', { name: /Cabinet quotes/ })).toHaveLength(1)
  })

  it('shows its chats, instructions, files, and settings', async () => {
    renderPage()
    expect(await screen.findByRole('heading', { name: 'Kitchen remodel' })).toBeInTheDocument()
    expect(await screen.findByRole('link', { name: /Cabinet quotes/ })).toHaveAttribute('href', '/chat?c=c1')
    expect(screen.getByRole('textbox', { name: 'Instructions' })).toHaveValue('Answers are for a small house.')
    expect(screen.getByText('quotes.md')).toBeInTheDocument()
    expect(screen.getByRole('switch', { name: 'Project memory' })).toHaveAttribute('aria-checked', 'true')
    expect(screen.getByRole('textbox', { name: 'Folder on the Toskar computer' })).toHaveValue('/work/kitchen')
  })

  it('starts a new chat in the project', async () => {
    renderPage()
    fireEvent.click(await screen.findByRole('button', { name: 'New chat' }))
    expect(screen.getByTestId('where').textContent).toBe('/chat?new=1&project=kitchen')
  })

  it('saves its name, instructions, memory, and defaults', async () => {
    renderPage()
    fireEvent.click(await screen.findByRole('button', { name: 'Rename' }))
    fireEvent.change(screen.getByRole('textbox', { name: 'Project name' }), { target: { value: 'Kitchen' } })
    // The name's Save comes first; the folder has its own.
    fireEvent.click(screen.getAllByRole('button', { name: 'Save' })[0])
    await waitFor(() => expect(api.updateProject).toHaveBeenCalledWith('kitchen', { name: 'Kitchen' }))

    fireEvent.change(screen.getByRole('textbox', { name: 'Instructions' }), { target: { value: 'Use metric units.' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save instructions' }))
    await waitFor(() => expect(api.updateProject).toHaveBeenCalledWith('kitchen', { instructions: 'Use metric units.' }))

    fireEvent.click(screen.getByRole('switch', { name: 'Project memory' }))
    await waitFor(() => expect(api.updateProject).toHaveBeenCalledWith('kitchen', { memory_off: true }))

    fireEvent.change(screen.getByRole('combobox', { name: 'Model' }), { target: { value: 'auto' } })
    await waitFor(() => expect(api.updateProject).toHaveBeenCalledWith('kitchen', { model_id: 'auto' }))
  })

  it("lets an Admin set the folder, and says why one isn't a folder", async () => {
    vi.mocked(api.updateProject).mockRejectedValueOnce(Object.assign(new Error('not a folder'), { code: 'NOT_A_FOLDER' }))
    renderPage()
    const field = await screen.findByRole('textbox', { name: 'Folder on the Toskar computer' })
    fireEvent.change(field, { target: { value: '/gone' } })
    fireEvent.click(screen.getAllByRole('button', { name: 'Save' })[0])
    await waitFor(() => expect(api.updateProject).toHaveBeenCalledWith('kitchen', { folder: '/gone' }))
    expect(await screen.findByRole('alert')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Remove' }))
    await waitFor(() => expect(api.updateProject).toHaveBeenCalledWith('kitchen', { folder: '' }))
  })

  it('hides the folder from a Member, and linked knowledge from a Visitor', async () => {
    vi.mocked(api.getMe).mockResolvedValue(me('member'))
    const { unmount } = renderPage()
    expect(await screen.findByRole('heading', { name: 'Knowledge' })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Folder' })).toBeNull()
    // A Member links existing sources but doesn't connect new ones.
    expect(screen.queryByRole('textbox', { name: /path/i })).toBeNull()
    unmount()

    vi.mocked(api.getMe).mockResolvedValue(me('visitor'))
    renderPage()
    expect(await screen.findByRole('heading', { name: 'Instructions' })).toBeInTheDocument()
    await waitFor(() => expect(screen.queryByRole('heading', { name: 'Knowledge' })).toBeNull())
    expect(screen.queryByRole('heading', { name: 'Folder' })).toBeNull()
  })

  it('adds and removes a file', async () => {
    vi.mocked(api.addProjectFile).mockResolvedValue({ ...kitchen, files: [...kitchen.files, { name: 'notes.txt', size: 5 }] })
    vi.mocked(api.removeProjectFile).mockResolvedValue(null)
    renderPage()
    const input = (await screen.findByLabelText('Add a file', { selector: 'input' })) as HTMLInputElement
    fireEvent.change(input, { target: { files: [new File(['hello'], 'notes.txt', { type: 'text/plain' })] } })
    await waitFor(() => expect(api.addProjectFile).toHaveBeenCalledWith('kitchen', { filename: 'notes.txt', text: 'hello' }))
    expect(await screen.findByText('notes.txt')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Remove quotes.md' }))
    await waitFor(() => expect(api.removeProjectFile).toHaveBeenCalledWith('kitchen', 'quotes.md'))
  })

  it('deletes the project, keeping its chats unless asked', async () => {
    renderPage()
    fireEvent.click(await screen.findByRole('button', { name: 'Delete project' }))
    const dialog = screen.getByRole('dialog', { name: 'Delete “Kitchen remodel”?' })
    expect(dialog).toHaveTextContent('Its chats move out of the project')
    fireEvent.click(screen.getByRole('checkbox', { name: 'Also delete its 2 chats' }))
    expect(dialog).toHaveTextContent("This can't be undone")
    fireEvent.click(screen.getAllByRole('button', { name: 'Delete project' }).at(-1)!)
    await waitFor(() => expect(api.deleteProject).toHaveBeenCalledWith('kitchen', { deleteChats: true }))
    expect(await screen.findByTestId('where')).toHaveTextContent('/chat')
  })

  it("says so when there's no such project", async () => {
    vi.mocked(api.getProject).mockResolvedValue(null)
    renderPage()
    expect(await screen.findByText(/This project isn't here/)).toBeInTheDocument()
  })
})

describe('NewProjectDialog (#1136)', () => {
  it('makes a project from its name', async () => {
    vi.mocked(api.createProject).mockResolvedValue({ ...kitchen, id: 'garden', name: 'Garden' })
    const onCreated = vi.fn()
    render(
      <QueryClientProvider client={new QueryClient()}>
        <NewProjectDialog onClose={vi.fn()} onCreated={onCreated} />
      </QueryClientProvider>,
    )
    expect(screen.getByRole('button', { name: 'Make project' })).toBeDisabled()
    fireEvent.change(screen.getByRole('textbox', { name: 'Name' }), { target: { value: ' Garden ' } })
    fireEvent.click(screen.getByRole('button', { name: 'Make project' }))
    await waitFor(() => expect(api.createProject).toHaveBeenCalledWith({ name: 'Garden' }))
    await waitFor(() => expect(onCreated.mock.calls[0][0].id).toBe('garden'))
  })
})

describe('MoveToProjectDialog (#1136)', () => {
  const garden: Project = { ...kitchen, id: 'garden', name: 'Garden' }
  function renderDialog(chat: Conversation) {
    const onClose = vi.fn()
    render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <MoveToProjectDialog conversation={chat} onClose={onClose} />
      </QueryClientProvider>,
    )
    return onClose
  }
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(api.listProjects).mockResolvedValue([kitchen, garden])
    vi.mocked(api.updateConversation).mockResolvedValue(chats[0])
  })

  it("moves a chat into another project, leaving out the one it's in", async () => {
    const onClose = renderDialog(chats[0])
    fireEvent.click(await screen.findByRole('button', { name: 'Garden' }))
    expect(screen.queryByRole('button', { name: 'Kitchen remodel' })).toBeNull()
    await waitFor(() => expect(api.updateConversation).toHaveBeenCalledWith('c1', { project_id: 'garden' }))
    await waitFor(() => expect(onClose).toHaveBeenCalled())
  })

  it('makes a new project and moves the chat into it', async () => {
    vi.mocked(api.createProject).mockResolvedValue({ ...kitchen, id: 'deck', name: 'Deck' })
    renderDialog({ id: 'c9', title: 'Loose chat', created_at: now, updated_at: now })
    await screen.findByRole('button', { name: 'Kitchen remodel' })
    fireEvent.change(screen.getByRole('textbox', { name: 'New project' }), { target: { value: 'Deck' } })
    fireEvent.click(screen.getByRole('button', { name: 'Make and move' }))
    await waitFor(() => expect(api.createProject).toHaveBeenCalledWith({ name: 'Deck' }))
    await waitFor(() => expect(api.updateConversation).toHaveBeenCalledWith('c9', { project_id: 'deck' }))
  })
})
