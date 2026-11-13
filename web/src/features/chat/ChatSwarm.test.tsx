import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { api, streamChat } from '@/lib/api'
import { useUIStore } from '@/stores/uiStore'
import type { Conversation, Message, Swarm, YggdrasilEvent } from '@/types/api'
import { stillMascot } from '@/test/stillMascot'
import { ChatPage } from './ChatPage'

// Swarms in the chat (#1135): "Use a swarm" for the next message, the
// agents' lanes from the event stream, their questions in the main chat,
// lanes folded above an answer after a reload, and an agent's own chat.
const listeners: ((event: YggdrasilEvent) => void)[] = []
vi.mock('@/lib/events', () => ({
  KNOWN_EVENT_TYPES: [],
  subscribeEvents: ({ onEvent }: { onEvent: (event: YggdrasilEvent) => void }) => {
    listeners.push(onEvent)
    return () => listeners.splice(listeners.indexOf(onEvent), 1)
  },
}))

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    streamChat: vi.fn(async () => undefined),
    api: {
      ...actual.api,
      getConversations: vi.fn(),
      listProjects: vi.fn(async () => []),
      getMessages: vi.fn(async () => []),
      getModels: vi.fn(async () => [{ id: 'm1', display_name: 'Model one', installed: true }]),
      getNodes: vi.fn(async () => [{ id: 'n2', name: 'Garage PC', os: 'linux', arch: 'amd64', status: 'online', is_local: false, paired: true }]),
      stopChat: vi.fn(async () => ({ stopped: true })),
      decideTool: vi.fn(async () => ({ status: 'ok' })),
      listSwarms: vi.fn(async () => []),
      getSwarm: vi.fn(),
      getSwarmConversations: vi.fn(async () => []),
    },
  }
})

const now = new Date().toISOString()
const plan: Conversation = { id: 'c1', title: 'Plan the trip', created_at: now, updated_at: now }

function emit(type: string, payload: Record<string, unknown>) {
  act(() => {
    for (const listener of [...listeners]) listener({ id: type, type, timestamp: now, payload })
  })
}

function started() {
  emit('swarm.started', {
    swarm_id: 's1',
    conversation_id: 'c1',
    mode: 'split',
    agents: [
      { agent: 1, conversation_id: 'a1', part: 'Flights', role: 'agent:1', model: 'm1', node_id: 'n2', status: 'running' },
      { agent: 2, conversation_id: 'a2', part: 'Hotels', role: 'agent:2', model: 'm1', node_id: 'n2', status: 'queued' },
    ],
  })
}

const record = (over: Partial<Swarm> = {}): Swarm => ({
  id: 's1',
  project_id: '',
  conversation_id: 'c1',
  mode: 'split',
  status: 'running',
  request: 'Plan the trip',
  budget: { agents: 4, tokens: 400000, minutes: 5 },
  agents: [
    { agent: 1, conversation_id: 'a1', role: 'agent:1', part: 'Find flights', model_id: 'm1', node_id: 'n2', status: 'running', tokens: 0, seconds: 0 },
    { agent: 2, conversation_id: 'a2', role: 'agent:2', part: 'Find hotels', model_id: 'm1', node_id: 'n2', status: 'queued', tokens: 0, seconds: 0 },
  ],
  tokens: 0,
  seconds: 0,
  created_at: now,
  ...over,
})

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

describe('Swarms in the chat (#1135)', () => {
  beforeEach(() => {
    useUIStore.getState().resetToDefaults()
    vi.stubGlobal('fetch', vi.fn(async () => new Response(null, { status: 404 })))
    stillMascot()
    vi.stubGlobal(
      'ResizeObserver',
      class {
        observe() {}
        unobserve() {}
        disconnect() {}
      },
    )
    vi.mocked(api.getConversations).mockResolvedValue([plan])
    vi.mocked(api.getSwarm).mockResolvedValue(record())
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.clearAllMocks()
  })

  it('asks for a swarm on the next message only, with a chip that removes it', async () => {
    renderChat('/chat?c=c1')
    const box = await screen.findByRole('textbox', { name: 'Message' }, { timeout: 4000 })
    fireEvent.click(screen.getByRole('button', { name: /Chat options/ }))
    fireEvent.change(screen.getByLabelText('Use a swarm'), { target: { value: 'race' } })
    expect(screen.getByText('Swarm: Race')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: "Don't use a swarm" }))
    expect(screen.queryByText('Swarm: Race')).toBeNull()

    fireEvent.change(screen.getByLabelText('Use a swarm'), { target: { value: 'split' } })
    fireEvent.change(box, { target: { value: 'Plan a week in Juneau' } })
    fireEvent.submit(box.closest('form')!)
    await waitFor(() => expect(streamChat).toHaveBeenCalledTimes(1))
    expect(vi.mocked(streamChat).mock.calls[0][0].body.swarm).toBe('split')
    // It's for that message: the next one goes without, unless asked again.
    await waitFor(() => expect(screen.queryByText('Swarm: Split')).toBeNull())
    expect(screen.getByLabelText('Use a swarm')).toHaveValue('')
  })

  it('shows lanes from the events, stops one agent, and opens another', async () => {
    renderChat('/chat?c=c1')
    await screen.findByRole('textbox', { name: 'Message' }, { timeout: 4000 })
    started()
    const lanes = await screen.findByRole('region', { name: 'Swarm' })
    expect(within(lanes).getByText('Split · 2 agents working')).toBeInTheDocument()
    const flights = within(lanes).getByRole('listitem', { name: 'Agent 1: Flights' })
    await waitFor(() => expect(within(flights).getByText('Garage PC · Model one')).toBeInTheDocument())
    expect(within(flights).getByText('Working')).toBeInTheDocument()
    expect(within(lanes).getByRole('listitem', { name: 'Agent 2: Hotels' })).toHaveTextContent('Queued')

    emit('swarm.agent.started', { swarm_id: 's1', conversation_id: 'c1', agent: 2, node_id: 'n2', model: 'm1' })
    emit('swarm.agent.progress', { swarm_id: 's1', conversation_id: 'c1', agent: 1, step: 1, tool: 'internet.search', summary: 'flights to Juneau' })
    expect(within(flights).getByText(/flights to Juneau/)).toBeInTheDocument()
    expect(within(lanes).getByRole('listitem', { name: 'Agent 2: Hotels' })).toHaveTextContent('Working')

    // Another swarm's events, or another chat's, leave these lanes alone.
    emit('swarm.agent.finished', { swarm_id: 's1', conversation_id: 'other', agent: 1, status: 'failed' })
    expect(within(flights).getByText('Working')).toBeInTheDocument()

    fireEvent.click(within(flights).getByRole('button', { name: 'Stop Agent 1: Flights' }))
    expect(api.stopChat).toHaveBeenCalledWith('a1')
    emit('swarm.agent.finished', { swarm_id: 's1', conversation_id: 'c1', agent: 1, status: 'stopped', summary: '', tokens: 10, seconds: 3 })
    expect(within(flights).getByText('Stopped')).toBeInTheDocument()
    expect(within(flights).queryByRole('button', { name: /^Stop/ })).toBeNull()

    emit('swarm.agent.finished', { swarm_id: 's1', conversation_id: 'c1', agent: 2, status: 'done', summary: 'Three hotels near the harbor.', tokens: 900, seconds: 40 })
    expect(within(lanes).getByText('Three hotels near the harbor.')).toBeInTheDocument()
    emit('swarm.merging', { swarm_id: 's1', conversation_id: 'c1', mode: 'split', agents: [2] })
    expect(within(lanes).getByText("Merging the agents' work…")).toBeInTheDocument()

    fireEvent.click(within(lanes).getByRole('button', { name: 'Open the chat for Agent 2: Hotels' }))
    await waitFor(() => expect(api.getMessages).toHaveBeenCalledWith('a2'))
  })

  it("shows a task's conflict, branch, checks, and whether it was applied", async () => {
    renderChat('/chat?c=c1')
    await screen.findByRole('textbox', { name: 'Message' }, { timeout: 4000 })
    started()
    emit('swarm.merging', {
      swarm_id: 's1',
      conversation_id: 'c1',
      agent: 2,
      branch: 'toskar/swarm/s1/agent-2',
      conflict: { agent: 2, branch: 'toskar/swarm/s1/agent-2', files: ['main.go'] },
    })
    const lanes = screen.getByRole('region', { name: 'Swarm' })
    expect(within(lanes).getByText(/Agent 2's changes conflict in main.go/)).toBeInTheDocument()
    vi.mocked(api.getSwarm).mockResolvedValue(
      record({
        status: 'done',
        folder: '/work/trip',
        branch: 'toskar/swarm/s1/merged',
        checks: 'passed',
        applied: true,
        conflicts: [{ agent: 2, branch: 'toskar/swarm/s1/agent-2', files: ['main.go'] }],
      }),
    )
    emit('swarm.finished', { swarm_id: 's1', conversation_id: 'c1', status: 'done', tokens: 100, seconds: 9 })
    expect(await within(lanes).findByText('Applied to your folder as uncommitted changes.')).toBeInTheDocument()
    expect(within(lanes).getByText('Result on branch toskar/swarm/s1/merged')).toBeInTheDocument()
    expect(within(lanes).getByText("The project's checks passed.")).toBeInTheDocument()
  })

  it("shows an agent's question in the main chat, and answers it for that agent only", async () => {
    renderChat('/chat?c=c1')
    await screen.findByRole('textbox', { name: 'Message' }, { timeout: 4000 })
    started()
    emit('tool.requested', {
      request_id: 'r1',
      tool_id: 'terminal',
      args: { command: 'go test ./...' },
      conversation_id: 'a2',
      swarm_id: 's1',
      agent: 'Agent 2: tests',
      agent_number: 2,
    })
    // A question from a swarm this chat didn't start isn't shown.
    emit('tool.requested', { request_id: 'r9', tool_id: 'terminal', conversation_id: 'x2', swarm_id: 's9', agent: 'Agent 1: other' })
    const asks = await screen.findByRole('list', { name: "Agents' questions" })
    expect(within(asks).getAllByRole('listitem')).toHaveLength(1)
    expect(within(asks).getByText('Agent 2: tests')).toBeInTheDocument()
    expect(within(asks).getByText(/covers only Agent 2: tests/)).toBeInTheDocument()
    expect(within(asks).queryByRole('button', { name: 'Allow for session' })).toBeNull()
    fireEvent.click(within(asks).getByRole('button', { name: 'Allow for this agent' }))
    await waitFor(() => expect(api.decideTool).toHaveBeenCalledWith({ request_id: 'r1', allow: true, allow_session: true }))
    await waitFor(() => expect(screen.queryByRole('list', { name: "Agents' questions" })).toBeNull())
    // The chat's own dialog isn't opened for an agent's question.
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('reads the questions agents were already waiting on when the chat opens', async () => {
    vi.mocked(api.getConversations).mockResolvedValue([{ ...plan, swarm_id: 's1' }])
    vi.mocked(api.getSwarm).mockResolvedValue(
      record({ waiting: [{ request_id: 'r2', tool_id: 'filesystem.write', conversation_id: 'a1', swarm_id: 's1', agent: 'Agent 1: Flights', agent_number: 1 }] }),
    )
    renderChat('/chat?c=c1')
    const asks = await screen.findByRole('list', { name: "Agents' questions" }, { timeout: 4000 })
    expect(within(asks).getByText('Agent 1: Flights')).toBeInTheDocument()
    fireEvent.click(within(asks).getByRole('button', { name: 'Deny' }))
    await waitFor(() => expect(api.decideTool).toHaveBeenCalledWith({ request_id: 'r2', allow: false, allow_session: false }))
    // The swarm still running shows its lanes from its record.
    expect(screen.getByRole('listitem', { name: 'Agent 1: Find flights' })).toBeInTheDocument()
  })

  it('stops the whole swarm with Stop, as any turn', async () => {
    vi.mocked(streamChat).mockImplementation(() => new Promise(() => undefined))
    renderChat('/chat?c=c1')
    const box = await screen.findByRole('textbox', { name: 'Message' }, { timeout: 4000 })
    fireEvent.change(box, { target: { value: 'Plan a week in Juneau' } })
    fireEvent.submit(box.closest('form')!)
    started()
    fireEvent.click(await screen.findByRole('button', { name: 'Stop' }))
    expect(api.stopChat).toHaveBeenCalledWith('c1')
  })

  it('folds the lanes above their answer after a reload', async () => {
    const before = new Date(Date.now() - 60_000).toISOString()
    const messages: Message[] = [
      { id: 'u1', conversation_id: 'c1', role: 'user', content: 'Plan a week in Juneau', created_at: before },
      { id: 'm1', conversation_id: 'c1', role: 'assistant', content: 'Here is the plan.', created_at: now },
    ]
    vi.mocked(api.getConversations).mockResolvedValue([{ ...plan, swarm_id: 's1' }])
    vi.mocked(api.getMessages).mockResolvedValue(messages)
    vi.mocked(api.getSwarm).mockResolvedValue(
      record({
        status: 'done',
        created_at: before,
        agents: record().agents!.map((a) => ({ ...a, status: 'done', summary: `${a.part}: done` })),
      }),
    )
    renderChat('/chat?c=c1')
    const toggle = await screen.findByRole('button', { name: /Split · worked with 2 agents/ }, { timeout: 4000 })
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByRole('listitem', { name: 'Agent 1: Find flights' })).toBeNull()
    fireEvent.click(toggle)
    expect(screen.getByRole('listitem', { name: 'Agent 1: Find flights' })).toHaveTextContent('Find flights: done')
  })

  it("says an agent's chat is part of a swarm, with the way back to the main chat", async () => {
    const agentChat: Conversation = { id: 'a2', title: 'Agent 2: Hotels', swarm_id: 's1', swarm_role: 'agent:2', created_at: now, updated_at: now }
    vi.mocked(api.listSwarms).mockResolvedValue([record()])
    vi.mocked(api.getSwarmConversations).mockResolvedValue([{ ...plan, swarm_id: 's1' }, agentChat])
    renderChat('/chat?c=a2')
    const back = await screen.findByRole('button', { name: 'Open the main chat' }, { timeout: 4000 })
    expect(back.closest('p')).toHaveTextContent('Agent 2 of a swarm')
    fireEvent.click(back)
    await waitFor(() => expect(api.getMessages).toHaveBeenCalledWith('c1'))
  })
})
