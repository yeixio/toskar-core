import i18n from '@/i18n'
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

const PURPOSES = ['general', 'coding', 'research', 'custom']

/** What a profile is for, in words, or its purpose id for one this app doesn't know. */
export function purposeLabel(purpose: string): string {
  return PURPOSES.includes(purpose) ? i18n.t(`profiles:purposes.${purpose}`) : purpose
}

/** Whether a profile uses the Team strategy (or the Team orchestrator of older versions). */
export function isTeamProfile(profile: Pick<AIProfile, 'orchestrator_id' | 'orchestration'> | null | undefined): boolean {
  if (!profile) return false
  return profile.orchestration?.strategy === 'team' || profile.orchestrator_id === 'team'
}

export type StrategyValue = '' | 'single' | 'planned' | 'team' | 'swarm'

/** The strategies, in order; each is profiles:strategies.<value, or "auto"> in the catalog. */
export const STRATEGY_VALUES: StrategyValue[] = ['', 'single', 'planned', 'team', 'swarm']

export function strategyOption(value: StrategyValue): { label: string; detail: string } {
  const key = value || 'auto'
  return { label: i18n.t(`profiles:strategies.${key}.label`), detail: i18n.t(`profiles:strategies.${key}.detail`) }
}

/** A profile's strategy: its short name, the title shown (with the strategy in advanced mode), and how it works. */
export function strategyLabel(
  profile: Pick<AIProfile, 'orchestrator_id' | 'orchestration'>,
  advanced: boolean,
): { name: string; title: string; detail: string } {
  const value = (isTeamProfile(profile) ? 'team' : (profile.orchestration?.strategy ?? '')) as StrategyValue
  const option = strategyOption(STRATEGY_VALUES.includes(value) ? value : '')
  const name =
    value === 'team'
      ? i18n.t('profiles:strategies.teamTitle')
      : value === ''
        ? i18n.t('profiles:strategies.automaticTitle')
        : option.label
  const title = advanced ? i18n.t('profiles:strategies.withStrategy', { title: name, strategy: option.label }) : name
  return { name, title, detail: option.detail }
}

export function computerSelectionLabel(mode: string): {
  title: string
  short: string
  detail: string
} {
  const key = mode === 'prefer_local' || mode === 'manual' ? mode : 'automatic'
  return {
    title: i18n.t(`profiles:computers.${key}.title`),
    short: i18n.t(`profiles:computers.${key}.short`),
    detail: i18n.t(`profiles:computers.${key}.detail`),
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
    { id: 'filesystem.read', label: i18n.t('profiles:chips.files') },
    { id: 'git.status', label: i18n.t('profiles:chips.git') },
    { id: 'terminal', label: i18n.t('profiles:chips.terminal') },
  ]
  const chips: { label: string; tone: 'ok' | 'ask' | 'off' }[] = []
  for (const row of rows) {
    const policy = byId.get(row.id)
    if (!policy) continue
    if (policy === 'deny') chips.push({ label: row.label, tone: 'off' })
    else if (policy === 'allow') chips.push({ label: i18n.t('profiles:chips.ok', { label: row.label }), tone: 'ok' })
    else chips.push({ label: i18n.t('profiles:chips.ask', { label: row.label }), tone: 'ask' })
  }
  return chips
}

export function roleDisplayName(role: string): string {
  if (!role) return i18n.t('profiles:roles.role')
  if (MODEL_ROLES.some((r) => r.role === role)) return i18n.t(`profiles:roles.${role}`)
  const slot = /^([a-z]+):(\d+)$/.exec(role)
  if (slot) return i18n.t('profiles:roles.numbered', { role: roleDisplayName(slot[1]), n: slot[2] })
  return role.charAt(0).toUpperCase() + role.slice(1)
}

export function roleHint(role: ModelRoleLike): string {
  const model = role.model_id ? shortModel(role.model_id) : i18n.t('profiles:roles.automaticModel')
  const computer = role.node_id ? i18n.t('profiles:roles.pinnedComputer') : i18n.t('profiles:roles.automaticComputer')
  return i18n.t('profiles:roles.hint', { model, computer })
}

/** The model roles a profile can assign (spec §20). An empty role uses the chat's model. */
// Each role's name is profiles:roles.<role> in the catalog.
export const MODEL_ROLES: { role: string }[] = [
  { role: 'assistant' },
  { role: 'fast' },
  { role: 'coding' },
  { role: 'planner' },
  { role: 'worker' },
  { role: 'reviewer' },
  { role: 'drafter' },
  { role: 'judge' },
]

export function roleHelp(role: string): string {
  const key = role.toLowerCase() === 'coordinator' ? 'planner' : role.toLowerCase()
  return MODEL_ROLES.some((r) => r.role === key) ? i18n.t(`profiles:roles.help.${key}`) : i18n.t('profiles:roles.help.other')
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
  return (['general', 'coding', 'research', 'blank'] as const).map((id) => ({
    id,
    title: i18n.t(`profiles:create.options.${id}.label`),
    description: i18n.t(`profiles:create.options.${id}.description`),
  }))
}

/**
 * A name no other profile has: the name itself, or "name (2)", "name (3)".
 * Core refuses a name that's taken, ignoring case, since two profiles called
 * General Assistant left people unsure which one a chat used.
 */
export function freeProfileName(name: string, profiles: Pick<AIProfile, 'name'>[]): string {
  const taken = new Set(profiles.map((p) => p.name.trim().toLowerCase()))
  const base = name.trim()
  let candidate = base
  for (let i = 2; taken.has(candidate.toLowerCase()); i++) {
    candidate = `${base} (${i})`
  }
  return candidate
}

/** The profile new chats start with: the one chosen, while it exists, or General Assistant. */
export function defaultProfileIdOf(chosen: string | undefined, profiles: Pick<AIProfile, 'id'>[]): string {
  if (chosen && profiles.some((p) => p.id === chosen)) return chosen
  return 'general-assistant'
}
