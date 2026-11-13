import { keepPreviousData, useQuery, useQueryClient } from '@tanstack/react-query'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { api } from '@/lib/api'
import type { Conversation, Swarm, ToolRequestedPayload, YggdrasilEvent } from '@/types/api'
import {
  type AgentApproval,
  type SwarmView,
  agentNumber,
  applySwarmEvent,
  mergeViews,
  swarmActive,
  viewFromSwarm,
} from './swarmState'

/** The swarms a chat's records are read from: its project's, or the one it names. */
async function chatSwarms(conversationId: string, projectId: string, ids: string[]): Promise<Swarm[]> {
  const listed = projectId ? (await api.listSwarms(projectId)).filter((s) => s.conversation_id === conversationId) : []
  const missing = ids.filter((id) => !listed.some((s) => s.id === id))
  const more = await Promise.all(missing.map((id) => api.getSwarm(id).catch(() => null)))
  return [...listed, ...more.filter((s): s is Swarm => Boolean(s))]
}

/**
 * A coordinator chat's swarms (#1135): their lanes, from the event stream
 * while they run and from their records after, and the questions their
 * agents are waiting on. ChatPage passes every event to onEvent.
 */
export function useChatSwarms(conversationId: string | null, conversation: Conversation | null) {
  const queryClient = useQueryClient()
  const [live, setLive] = useState<Record<string, SwarmView>>({})
  const [asked, setAsked] = useState<AgentApproval[]>([])
  // Why a swarm asked for ran differently, such as as one agent, while the turn runs.
  const [note, setNote] = useState<{ reason: string; text: string } | null>(null)
  const answered = useRef(new Set<string>())

  // Another chat starts with nothing from this one.
  useEffect(() => {
    setLive({})
    setAsked([])
    setNote(null)
  }, [conversationId])

  // An agent's chat isn't a coordinator: its own lanes would be the swarm's.
  const coordinator = conversation && !conversation.swarm_role ? conversation : null
  const ids = useMemo(() => {
    const out = new Set(Object.keys(live))
    if (coordinator?.swarm_id) out.add(coordinator.swarm_id)
    return [...out].sort()
  }, [live, coordinator?.swarm_id])
  const projectId = coordinator?.project_id ?? ''
  // Read again when a swarm ends, even while an earlier read is under way.
  const [reads, setReads] = useState(0)
  const recordsQuery = useQuery({
    queryKey: ['swarms', 'chat', conversationId, projectId, ids.join(','), reads],
    queryFn: () => chatSwarms(conversationId!, projectId, ids),
    enabled: Boolean(conversationId) && ids.length > 0,
    // The last read stays in view while the next one comes.
    placeholderData: (previous, query) => (query?.queryKey[2] === conversationId ? keepPreviousData(previous) : undefined),
    retry: false,
  })
  const records = recordsQuery.data

  const views = useMemo(() => {
    const byId = new Map<string, SwarmView>()
    for (const swarm of records ?? []) byId.set(swarm.id, viewFromSwarm(swarm))
    const out: SwarmView[] = []
    for (const id of new Set([...byId.keys(), ...Object.keys(live)])) {
      const view = mergeViews(byId.get(id), live[id])
      if (view && view.lanes.length > 0) out.push(view)
    }
    return out
  }, [records, live])

  // Questions a running swarm's agents were already waiting on.
  const waiting = useMemo(() => {
    const out: AgentApproval[] = []
    for (const swarm of records ?? []) {
      if (!swarmActive(swarm.status)) continue
      for (const ask of swarm.waiting ?? []) {
        if (!ask.request_id || !ask.tool_id || ask.conversation_id === conversationId) continue
        out.push({ ...ask, swarm_id: ask.swarm_id || swarm.id } as AgentApproval)
      }
    }
    return out
  }, [records, conversationId])
  const approvals = useMemo(() => {
    const out: AgentApproval[] = []
    for (const ask of [...waiting, ...asked]) {
      if (answered.current.has(ask.request_id) || out.some((a) => a.request_id === ask.request_id)) continue
      out.push(ask)
    }
    return out.sort((a, b) => (a.agent_number ?? 0) - (b.agent_number ?? 0))
  }, [waiting, asked])

  const refresh = useCallback(() => {
    setReads((n) => n + 1)
    void queryClient.invalidateQueries({ queryKey: ['swarms', 'project'] })
  }, [queryClient])

  const context = useRef({ conversationId, ids, refresh })
  context.current = { conversationId, ids, refresh }

  const onEvent = useCallback((event: YggdrasilEvent) => {
    const { conversationId, ids, refresh } = context.current
    if (!conversationId) return
    const p = event.payload ?? {}
    const swarmId = typeof p.swarm_id === 'string' ? p.swarm_id : ''
    if (event.type === 'swarm.note') {
      if (p.conversation_id === conversationId && typeof p.reason === 'string') {
        setNote({ reason: p.reason, text: typeof p.note === 'string' ? p.note : '' })
      }
      return
    }
    if (event.type.startsWith('swarm.')) {
      if (p.conversation_id !== conversationId || !swarmId) return
      setLive((current) => applySwarmEvent(current, event))
      // The record has a task's branch, checks, and whether it was applied.
      if (event.type === 'swarm.finished') refresh()
      return
    }
    if (event.type === 'tool.requested') {
      const ask = p as unknown as ToolRequestedPayload
      // The coordinator's own questions, such as applying the change, ask as any chat's do.
      if (!swarmId || !ids.includes(swarmId) || ask.conversation_id === conversationId) return
      if (!ask.request_id || !ask.tool_id) return
      setAsked((current) => (current.some((a) => a.request_id === ask.request_id) ? current : [...current, ask as AgentApproval]))
      return
    }
    if (event.type === 'tool.decided') {
      const requestId = typeof p.request_id === 'string' ? p.request_id : ''
      if (!requestId) return
      answered.current.add(requestId)
      setAsked((current) => current.filter((a) => a.request_id !== requestId))
      return
    }
    // Stopping the coordinator stops its agents; their lanes say so as they finish.
    if ((event.type === 'chat.complete' || event.type === 'chat.stopped') && p.conversation_id === conversationId) {
      setNote(null)
      refresh()
    }
  }, [])

  /** An answered question leaves the list at once; tool.decided also says so. */
  const markAnswered = useCallback((requestId: string) => {
    answered.current.add(requestId)
    setAsked((current) => current.filter((a) => a.request_id !== requestId))
  }, [])

  return { views, approvals, note, onEvent, markAnswered }
}

/**
 * An agent's chat opened on its own (#1135): history leaves agents' chats
 * out, so it's found through the person's swarms. Returns the chat, its
 * agent number, and the coordinator's chat.
 */
export function useAgentChat(conversationId: string | null, enabled: boolean) {
  const swarmsQuery = useQuery({
    queryKey: ['swarms', 'agent', conversationId],
    queryFn: () => api.listSwarms(),
    enabled: enabled && Boolean(conversationId),
    retry: false,
  })
  const swarm = swarmsQuery.data?.find((s) => (s.agents ?? []).some((a) => a.conversation_id === conversationId))
  const chatsQuery = useQuery({
    queryKey: ['conversations', 'swarm', swarm?.id],
    queryFn: () => api.getSwarmConversations(swarm!.id),
    enabled: enabled && Boolean(swarm),
    retry: false,
  })
  if (!enabled || !swarm || !conversationId) return null
  const conversation = chatsQuery.data?.find((c) => c.id === conversationId) ?? null
  const agent = swarm.agents?.find((a) => a.conversation_id === conversationId)
  return {
    conversation,
    agent: agent?.agent || agentNumber(conversation?.swarm_role),
    coordinatorId: swarm.conversation_id,
  }
}
