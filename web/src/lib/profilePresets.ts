import type { AIProfile, Purpose, Recommendation } from '@/types/api'

const defaultTools: AIProfile['tools'] = [
  { tool_id: 'internet.search', policy: 'allow' },
  { tool_id: 'internet.open', policy: 'allow' },
  { tool_id: 'filesystem.search', policy: 'allow' },
  { tool_id: 'filesystem.read', policy: 'allow' },
  { tool_id: 'filesystem.write', policy: 'allow' },
  { tool_id: 'terminal', policy: 'allow' },
  { tool_id: 'git.status', policy: 'allow' },
  { tool_id: 'git.diff', policy: 'allow' },
  { tool_id: 'git.log', policy: 'allow' },
  { tool_id: 'git.show', policy: 'allow' },
  { tool_id: 'git.add', policy: 'allow' },
  { tool_id: 'git.commit', policy: 'allow' },
  { tool_id: 'git.push', policy: 'allow' },
]

const codingTools: AIProfile['tools'] = [
  { tool_id: 'internet.search', policy: 'allow' },
  { tool_id: 'internet.open', policy: 'allow' },
  { tool_id: 'filesystem.search', policy: 'allow' },
  { tool_id: 'filesystem.read', policy: 'allow' },
  { tool_id: 'filesystem.write', policy: 'allow' },
  { tool_id: 'terminal', policy: 'allow' },
  { tool_id: 'git.status', policy: 'allow' },
  { tool_id: 'git.diff', policy: 'allow' },
  { tool_id: 'git.log', policy: 'allow' },
  { tool_id: 'git.show', policy: 'allow' },
  { tool_id: 'git.add', policy: 'allow' },
  { tool_id: 'git.commit', policy: 'allow' },
  { tool_id: 'git.push', policy: 'allow' },
]

const researchTools: AIProfile['tools'] = [
  { tool_id: 'internet.search', policy: 'allow' },
  { tool_id: 'internet.open', policy: 'allow' },
  { tool_id: 'filesystem.search', policy: 'allow' },
  { tool_id: 'filesystem.read', policy: 'allow' },
  { tool_id: 'filesystem.write', policy: 'allow' },
  { tool_id: 'terminal', policy: 'deny' },
  { tool_id: 'git.status', policy: 'allow' },
  { tool_id: 'git.diff', policy: 'deny' },
  { tool_id: 'git.add', policy: 'deny' },
  { tool_id: 'git.commit', policy: 'deny' },
  { tool_id: 'git.push', policy: 'deny' },
]

const offlineTools: AIProfile['tools'] = defaultTools.map((tool) =>
  tool.tool_id.startsWith('filesystem.')
    ? { ...tool, policy: 'allow' as const }
    : { ...tool, policy: 'deny' as const },
)

export function purposeToApiQuery(purpose: Purpose): string {
  if (purpose === 'coding') {
    return 'coding'
  }
  return purpose
}

export function buildProfileFromRecommendation(
  purpose: Purpose,
  recommendation: Recommendation,
): Omit<AIProfile, 'id'> {
  const base = profileTemplate(purpose)
  return {
    ...base,
    roles: recommendation.roles,
  }
}

export function profileTemplateFromPurpose(
  purpose: Purpose,
): Omit<AIProfile, 'id'> {
  const base = profileTemplate(purpose)
  return {
    ...base,
    roles: defaultRolesForPurpose(purpose),
  }
}

/** Minimal custom profile for “Blank” create flow. */
export function blankProfileTemplate(): Omit<AIProfile, 'id'> {
  return {
    name: 'New profile',
    purpose: 'custom',
    orchestrator_id: 'simple',
    node_policy: { mode: 'automatic' },
    roles: [{ role: 'assistant', model_id: '', required: false }],
    tools: offlineTools,
  }
}

function defaultRolesForPurpose(purpose: Purpose): AIProfile['roles'] {
  switch (purpose) {
    case 'coding':
      return [
        { role: 'planner', model_id: '', required: false },
        { role: 'worker', model_id: '', required: false },
        { role: 'reviewer', model_id: '', required: false },
      ]
    case 'research':
      return [{ role: 'assistant', model_id: '', required: false }]
    case 'custom':
      return [{ role: 'assistant', model_id: '', required: false }]
    default:
      return [{ role: 'assistant', model_id: '', required: false }]
  }
}

function profileTemplate(purpose: Purpose): Omit<AIProfile, 'id' | 'roles'> {
  switch (purpose) {
    case 'coding':
      return {
        name: 'Programming',
        purpose: 'coding',
        orchestrator_id: 'simple',
        orchestration: { strategy: 'team' },
        node_policy: { mode: 'automatic' },
        tools: codingTools,
      }
    case 'research':
      return {
        name: 'Research',
        purpose: 'research',
        orchestrator_id: 'simple',
        node_policy: { mode: 'automatic' },
        tools: researchTools,
      }
    case 'custom':
      return {
        name: 'Custom',
        purpose: 'custom',
        orchestrator_id: 'simple',
        node_policy: { mode: 'manual' },
        tools: offlineTools,
      }
    default:
      return {
        name: 'General Assistant',
        purpose: 'general',
        orchestrator_id: 'simple',
        node_policy: { mode: 'automatic' },
        tools: defaultTools,
      }
  }
}

export const presetOptions: {
  id: Purpose
  title: string
  description: string
  detail?: string
}[] = [
  {
    id: 'general',
    title: 'General Assistant',
    description: 'Everyday questions, writing help, and local-first conversation.',
    detail:
      'Uses a Simple orchestrator (one assistant). Good default when you want chat plus optional file or git tools.',
  },
  {
    id: 'coding',
    title: 'Programming',
    description:
      'Code help that can use all your paired computers automatically when models are installed on them.',
    detail:
      'Uses Team (coordinator → worker → reviewer). Pin a worker to another Mac when you want a visible two-machine demo.',
  },
  {
    id: 'research',
    title: 'Research',
    description: 'Long-form reading, summarization, and careful reasoning.',
    detail:
      'Simple orchestrator with a cautious tool set — terminal is denied by default so it stays read-focused.',
  },
  {
    id: 'custom',
    title: 'Custom',
    description: 'Pick models and orchestration yourself in advanced mode.',
    detail:
      'Starts blank-ish so you can choose Simple or Team, pin roles, and set tool permissions from scratch.',
  },
]
