import type { OrchestrationPolicy, SwarmPolicy } from '@/types/api'

/** Which controls go in which section of the profile editor (spec §20). */
export const ORCHESTRATION_KEYS: (keyof OrchestrationPolicy)[] = [
  'effort',
  'planning',
  'parallel',
  'verification',
  'max_workers',
  'max_tool_calls',
]
export const MEMORY_KEYS: (keyof OrchestrationPolicy)[] = ['memory', 'context_share']
export const EXECUTION_KEYS: (keyof OrchestrationPolicy)[] = ['fallback', 'retries', 'timeout_seconds']

/** The swarm's budgets, with the ranges Core accepts (#1135). */
export const SWARM_NUMBERS: { key: 'agents' | 'tokens' | 'minutes'; min: number; max: number; step?: number }[] = [
  { key: 'agents', min: 2, max: 8 },
  { key: 'tokens', min: 10_000, max: 4_000_000, step: 10_000 },
  { key: 'minutes', min: 1, max: 240 },
]

/** The most models a swarm takes, one per agent. */
export const MAX_MODELS = 8

/** Drops a swarm's empty controls, so it keeps every default it doesn't change. */
export function cleanSwarm(swarm: SwarmPolicy | undefined): SwarmPolicy | undefined {
  if (!swarm) return undefined
  const out: SwarmPolicy = {}
  if (swarm.mode) out.mode = swarm.mode
  if (swarm.models?.length) out.models = swarm.models
  for (const { key } of SWARM_NUMBERS) {
    const v = swarm[key]
    if (typeof v === 'number' && v > 0 && !Number.isNaN(v)) out[key] = v
  }
  return Object.keys(out).length > 0 ? out : undefined
}
