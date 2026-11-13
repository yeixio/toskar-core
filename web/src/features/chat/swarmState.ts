import type { Message, Swarm, SwarmConflict, ToolRequestedPayload, YggdrasilEvent } from '@/types/api'

/** One of an agent's latest tool calls, from swarm.agent.progress. */
export type LaneStep = { step: number; tool: string; summary: string }

/** One agent's lane in a swarm (#1135). */
export type Lane = {
  agent: number
  conversationId: string
  /** Its part's title from the event stream, or its instructions from the record. */
  part: string
  modelId?: string
  nodeId?: string
  status: string
  /** The latest few steps, oldest first. */
  steps: LaneStep[]
  summary?: string
  tokens?: number
  seconds?: number
  branch?: string
  checks?: string
}

/** A swarm as the chat shows it: from its events, its record, or both. */
export type SwarmView = {
  id: string
  conversationId: string
  mode: string
  status: string
  lanes: Lane[]
  /** What is being merged now; in a task, the agent and branch. */
  merging?: { agent?: number; branch?: string } | null
  conflicts: SwarmConflict[]
  winner?: number
  folder?: string
  branch?: string
  checks?: string
  applied?: boolean
  createdAt?: string
}

/** How many of an agent's latest steps its lane shows. */
export const LANE_STEPS = 3

/** Whether an agent is still at work, so its lane offers Stop. */
export function laneActive(status: string): boolean {
  return status === 'running' || status === 'queued'
}

/** Whether a swarm is still at work. */
export function swarmActive(status: string): boolean {
  return status === 'running' || status === 'merging'
}

const str = (v: unknown) => (typeof v === 'string' ? v : '')
const num = (v: unknown) => (typeof v === 'number' && Number.isFinite(v) ? v : Number(v) || 0)

function updateLane(view: SwarmView, agent: number, patch: (lane: Lane) => Lane): SwarmView {
  return { ...view, lanes: view.lanes.map((lane) => (lane.agent === agent ? patch(lane) : lane)) }
}

/** A swarm's view from its record (GET /swarms/{id}). */
export function viewFromSwarm(swarm: Swarm): SwarmView {
  return {
    id: swarm.id,
    conversationId: swarm.conversation_id,
    mode: swarm.mode,
    status: swarm.status,
    lanes: (swarm.agents ?? []).map((a) => ({
      agent: a.agent,
      conversationId: a.conversation_id,
      part: a.part,
      modelId: a.model_id,
      nodeId: a.node_id,
      status: a.status,
      steps: [],
      summary: a.summary,
      tokens: a.tokens,
      seconds: a.seconds,
      branch: a.branch,
      checks: a.checks,
    })),
    merging: null,
    conflicts: swarm.conflicts ?? [],
    winner: swarm.winner,
    folder: swarm.folder,
    branch: swarm.branch,
    checks: swarm.checks,
    applied: swarm.applied,
    createdAt: swarm.created_at,
  }
}

/**
 * The record and the events together. The record has a task's outcome and
 * when the swarm started; the events have the parts' short titles and the
 * steps, and can be newer than the record while it runs.
 */
export function mergeViews(fetched: SwarmView | undefined, live: SwarmView | undefined): SwarmView | undefined {
  if (!fetched) return live
  if (!live) return fetched
  const finished = !swarmActive(fetched.status)
  const lanes = fetched.lanes.map((lane) => {
    const seen = live.lanes.find((l) => l.agent === lane.agent)
    if (!seen) return lane
    // The events are newer than a record read while the swarm runs; a
    // finished lane's status is settled either way.
    const status = !laneActive(seen.status) || laneActive(lane.status) ? seen.status : lane.status
    return { ...lane, part: seen.part || lane.part, steps: seen.steps, status, summary: lane.summary || seen.summary }
  })
  // Agents the record doesn't have yet, such as a fix-up agent.
  for (const lane of live.lanes) if (!lanes.some((l) => l.agent === lane.agent)) lanes.push(lane)
  return {
    ...fetched,
    lanes,
    status: finished ? fetched.status : live.status,
    merging: finished ? null : live.merging,
    conflicts: fetched.conflicts.length > 0 ? fetched.conflicts : live.conflicts,
    winner: fetched.winner || live.winner,
  }
}

/** A swarm event applied to the chat's swarms, keyed by id. Other events leave them as they are. */
export function applySwarmEvent(views: Record<string, SwarmView>, event: YggdrasilEvent): Record<string, SwarmView> {
  const p = event.payload ?? {}
  const id = str(p.swarm_id)
  if (!id || !event.type.startsWith('swarm.')) return views
  const view = views[id]
  switch (event.type) {
    case 'swarm.started': {
      const agents = Array.isArray(p.agents) ? (p.agents as Record<string, unknown>[]) : []
      return {
        ...views,
        [id]: {
          id,
          conversationId: str(p.conversation_id),
          mode: str(p.mode) || 'split',
          status: 'running',
          lanes: agents.map((a) => ({
            agent: num(a.agent),
            conversationId: str(a.conversation_id),
            part: str(a.part),
            modelId: str(a.model) || undefined,
            nodeId: str(a.node_id) || undefined,
            status: str(a.status) || 'running',
            steps: [],
            branch: str(a.branch) || undefined,
          })),
          merging: null,
          conflicts: [],
        },
      }
    }
    case 'swarm.agent.started':
      if (!view) return views
      return {
        ...views,
        [id]: updateLane(view, num(p.agent), (lane) => ({
          ...lane,
          status: 'running',
          nodeId: str(p.node_id) || lane.nodeId,
          modelId: str(p.model) || lane.modelId,
        })),
      }
    case 'swarm.agent.progress':
      if (!view) return views
      return {
        ...views,
        [id]: updateLane(view, num(p.agent), (lane) => ({
          ...lane,
          steps: [...lane.steps, { step: num(p.step), tool: str(p.tool), summary: str(p.summary) }].slice(-LANE_STEPS),
        })),
      }
    case 'swarm.agent.finished':
      if (!view) return views
      return {
        ...views,
        [id]: updateLane(view, num(p.agent), (lane) => ({
          ...lane,
          status: str(p.status) || 'done',
          summary: str(p.summary) || lane.summary,
          tokens: num(p.tokens),
          seconds: num(p.seconds),
        })),
      }
    case 'swarm.merging': {
      if (!view) return views
      const conflict = p.conflict as Record<string, unknown> | undefined
      const conflicts = conflict
        ? [
            ...view.conflicts,
            {
              agent: num(conflict.agent),
              branch: str(conflict.branch),
              files: Array.isArray(conflict.files) ? (conflict.files as string[]) : [],
            },
          ]
        : view.conflicts
      const agent = p.agent === undefined ? undefined : num(p.agent)
      return {
        ...views,
        [id]: { ...view, status: 'merging', merging: { agent, branch: str(p.branch) || undefined }, conflicts },
      }
    }
    case 'swarm.finished':
      if (!view) return views
      return {
        ...views,
        [id]: { ...view, status: str(p.status) || 'done', merging: null, winner: num(p.winner) || view.winner },
      }
    default:
      return views
  }
}

/**
 * Which answer each swarm belongs to: the first answer saved after it
 * started. A message doesn't name its swarm, and a chat's swarm_id is only
 * its latest, so the times link them.
 */
export function swarmsByAnswer(messages: Pick<Message, 'id' | 'role' | 'created_at'>[], views: SwarmView[]): Map<string, SwarmView[]> {
  const out = new Map<string, SwarmView[]>()
  const answers = messages
    .filter((m) => m.role === 'assistant' && !m.id.startsWith('optimistic-'))
    .map((m) => ({ id: m.id, at: Date.parse(m.created_at) }))
  for (const view of views) {
    if (!view.createdAt || view.lanes.length === 0 || swarmActive(view.status)) continue
    const started = Date.parse(view.createdAt)
    const answer = answers.find((a) => a.at >= started)
    if (!answer) continue
    out.set(answer.id, [...(out.get(answer.id) ?? []), view])
  }
  return out
}

/** An agent's question waiting in the coordinator's chat. */
export type AgentApproval = ToolRequestedPayload & { request_id: string; tool_id: string }

/** The agent's number in its role, agent:N; 0 for any other role. */
export function agentNumber(role: string | undefined): number {
  const match = /^agent:(\d+)$/.exec(role ?? '')
  return match ? Number(match[1]) : 0
}
