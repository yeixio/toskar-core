import type { AIProfile, Purpose, ToolPolicy } from '@/types/api'

/** Built-in profile template IDs (must match server presets). */
export const BUILT_IN_PROFILE_IDS = new Set([
  'general-assistant',
  'programming',
  'research',
  'custom',
])

export function isBuiltInProfile(id: string): boolean {
  return BUILT_IN_PROFILE_IDS.has(id)
}

export const PURPOSE_LABELS: Record<string, string> = {
  general: 'Everyday conversation',
  coding: 'Programming & code help',
  research: 'Research & long-form reading',
  custom: 'Custom setup',
}

/** Whether a profile uses the Team strategy (or the Team orchestrator of older versions). */
export function isTeamProfile(profile: Pick<AIProfile, 'orchestrator_id' | 'orchestration'> | null | undefined): boolean {
  if (!profile) return false
  return profile.orchestration?.strategy === 'team' || profile.orchestrator_id === 'team'
}

export const STRATEGY_OPTIONS: { value: '' | 'single' | 'planned' | 'team'; label: string; detail: string }[] = [
  {
    value: '',
    label: 'Auto',
    detail: 'Yggdrasil decides: quick questions get one model, requests with several parts are planned and checked.',
  },
  { value: 'single', label: 'Single model', detail: 'One model answers every request, without a plan.' },
  {
    value: 'planned',
    label: 'Planner + workers',
    detail: 'Requests with several parts are always worked through in parts before the answer is written.',
  },
  {
    value: 'team',
    label: 'Team',
    detail:
      'A planner splits each request, workers do the parts (on other computers when they can), and a reviewer checks the answer. Quick questions are still answered directly.',
  },
]

export function strategyLabel(
  profile: Pick<AIProfile, 'orchestrator_id' | 'orchestration'>,
  advanced: boolean,
): { title: string; detail: string } {
  const value = isTeamProfile(profile) ? 'team' : (profile.orchestration?.strategy ?? '')
  const option = STRATEGY_OPTIONS.find((o) => o.value === value) ?? STRATEGY_OPTIONS[0]
  const title = value === 'team' ? 'AI team' : value === '' ? 'Automatic' : option.label
  return { title: advanced ? `${title} · Strategy: ${option.label}` : title, detail: option.detail }
}

export function computerSelectionLabel(mode: string): {
  title: string
  short: string
  detail: string
} {
  switch (mode) {
    case 'prefer_local':
      return {
        title: 'Prefer this computer',
        short: 'This computer first',
        detail:
          'Tries to run on this machine first. Falls back to a paired computer when a model is not installed here.',
      }
    case 'manual':
      return {
        title: 'Custom',
        short: 'Custom',
        detail:
          'Uses role pins for placement. Ideal when you want a specific step on a specific machine.',
      }
    default:
      return {
        title: 'Automatic',
        short: 'Automatic',
        detail:
          'Places each step on whichever paired computer already has the model. No IP or port setup.',
      }
  }
}

export function toolSummary(tools: ToolPolicy[] | undefined): {
  enabled: number
  ask: number
  allow: number
  deny: number
} {
  const list = tools ?? []
  let ask = 0
  let allow = 0
  let deny = 0
  for (const t of list) {
    if (t.policy === 'deny') deny += 1
    else if (t.policy === 'ask' || t.policy === 'allow-for-session') ask += 1
    else allow += 1
  }
  return { enabled: list.length - deny, ask, allow, deny }
}

/** Compact tool chips for the card face (first few interesting ones). */
export function toolChipPreview(tools: ToolPolicy[] | undefined): {
  label: string
  tone: 'ok' | 'ask' | 'off'
}[] {
  const byId = new Map((tools ?? []).map((t) => [t.tool_id, t.policy]))
  const rows: { id: string; label: string }[] = [
    { id: 'filesystem.read', label: 'Files' },
    { id: 'git.status', label: 'Git' },
    { id: 'terminal', label: 'Terminal' },
  ]
  const chips: { label: string; tone: 'ok' | 'ask' | 'off' }[] = []
  for (const row of rows) {
    const policy = byId.get(row.id)
    if (!policy) continue
    if (policy === 'deny') chips.push({ label: row.label, tone: 'off' })
    else if (policy === 'allow') chips.push({ label: `${row.label} ✓`, tone: 'ok' })
    else chips.push({ label: `${row.label} Ask`, tone: 'ask' })
  }
  return chips
}

export function roleDisplayName(role: string): string {
  if (!role) return 'Role'
  const known = MODEL_ROLES.find((r) => r.role === role)
  if (known) return known.label
  const slot = /^([a-z]+):(\d+)$/.exec(role)
  if (slot) return `${roleDisplayName(slot[1])} ${slot[2]}`
  return role.charAt(0).toUpperCase() + role.slice(1)
}

export function roleHint(role: ModelRoleLike): string {
  const model = role.model_id ? shortModel(role.model_id) : 'Automatic model'
  const computer = role.node_id ? 'Pinned computer' : 'Automatic computer'
  return `${model} · ${computer}`
}

/** The model roles a profile can assign (spec §20). An empty role uses the chat's model. */
export const MODEL_ROLES: { role: string; label: string }[] = [
  { role: 'assistant', label: 'Primary' },
  { role: 'fast', label: 'Fast' },
  { role: 'coding', label: 'Coding' },
  { role: 'planner', label: 'Planner' },
  { role: 'worker', label: 'Worker' },
  { role: 'reviewer', label: 'Reviewer' },
]

export function roleHelp(role: string): string {
  switch (role.toLowerCase()) {
    case 'assistant':
      return 'Writes the answer. Automatic uses the model chosen in the chat.'
    case 'fast':
      return 'Answers quick questions when the chat is on Auto.'
    case 'coding':
      return 'Answers coding requests when the chat is on Auto.'
    case 'planner':
    case 'coordinator':
      return 'Splits a request into parts.'
    case 'worker':
      return 'Works on one part of a plan. Parts can run on different computers at once.'
    case 'reviewer':
      return 'Checks the answer before you see it.'
    default:
      return 'A named step in this profile’s workflow.'
  }
}

type ModelRoleLike = { role: string; model_id?: string; node_id?: string }

function shortModel(id: string): string {
  const base = id.split('/').pop() || id
  return base.length > 28 ? `${base.slice(0, 26)}…` : base
}

export function purposeIcon(purpose: string): 'general' | 'coding' | 'research' | 'custom' {
  if (purpose === 'coding' || purpose === 'research' || purpose === 'custom') {
    return purpose
  }
  return 'general'
}

export function sortProfilesForDisplay(profiles: AIProfile[]): AIProfile[] {
  const purposeOrder = ['general', 'coding', 'research', 'custom']
  return [...profiles].sort((a, b) => {
    const aBuilt = isBuiltInProfile(a.id) ? 0 : 1
    const bBuilt = isBuiltInProfile(b.id) ? 0 : 1
    if (aBuilt !== bBuilt) return aBuilt - bBuilt
    const ap = purposeOrder.indexOf(a.purpose)
    const bp = purposeOrder.indexOf(b.purpose)
    if (ap !== bp) return (ap === -1 ? 99 : ap) - (bp === -1 ? 99 : bp)
    return a.name.localeCompare(b.name)
  })
}

export type ProfileFilter = 'all' | 'builtin' | 'custom'

export function filterProfiles(
  profiles: AIProfile[],
  filter: ProfileFilter,
): AIProfile[] {
  if (filter === 'builtin') return profiles.filter((p) => isBuiltInProfile(p.id))
  if (filter === 'custom') return profiles.filter((p) => !isBuiltInProfile(p.id))
  return profiles
}

export type CreateStartFrom = Purpose | 'blank'

export function createStartOptions(): {
  id: CreateStartFrom
  title: string
  description: string
}[] {
  return [
    {
      id: 'general',
      title: 'General Assistant',
      description: 'Everyday questions and local-first chat.',
    },
    {
      id: 'coding',
      title: 'Programming',
      description: 'Code help with an AI team across your computers.',
    },
    {
      id: 'research',
      title: 'Research',
      description: 'Long-form reading and careful reasoning.',
    },
    {
      id: 'blank',
      title: 'Blank',
      description: 'Start empty and set roles, tools, and computers yourself.',
    },
  ]
}
