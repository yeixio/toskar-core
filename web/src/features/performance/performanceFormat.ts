import type { GenerationRun, GenerationRoleStep, Model } from '@/types/api'

export function formatMs(ms: number): string {
  if (!ms || ms <= 0) return '—'
  if (ms < 1000) return `${ms.toFixed(0)} ms`
  return `${(ms / 1000).toFixed(1)} s`
}

export function formatRate(n: number): string {
  if (!n || n <= 0) return '—'
  return n >= 100 ? `${n.toFixed(0)}` : `${n.toFixed(1)}`
}

export function formatWhen(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return '—'
  return d.toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

export function formatRoleLabel(role: string): string {
  if (!role) return 'Role'
  // A plan's worker slots are "worker:1", "worker:2", …
  const slot = /^([a-z]+):(\d+)$/.exec(role)
  if (slot) return `${slot[1].charAt(0).toUpperCase()}${slot[1].slice(1)} ${slot[2]}`
  return role.charAt(0).toUpperCase() + role.slice(1)
}

export function modelDisplayName(models: Model[], id: string): string {
  return models.find((m) => m.id === id)?.display_name ?? id
}

/** Ordered unique computer names for a run (route). */
export function nodeRoute(run: GenerationRun): string[] {
  const names: string[] = []
  const seen = new Set<string>()
  for (const step of run.role_steps ?? []) {
    const label = step.node_name || step.node_id
    if (!label || seen.has(label)) continue
    seen.add(label)
    names.push(label)
  }
  return names
}

export function routeLabel(run: GenerationRun): string {
  const names = nodeRoute(run)
  if (names.length === 0) {
    return run.cross_machine ? 'Multiple computers' : 'This computer'
  }
  if (names.length === 1) return names[0]
  return names.join(' → ')
}

export type MetricLabels = {
  speed: string
  prompt: string
  firstResponse: string
  total: string
  teamRun: string
}

export function metricLabels(advanced: boolean): MetricLabels {
  if (advanced) {
    return {
      speed: 'Eval tok/s',
      prompt: 'Prompt eval',
      firstResponse: 'TTFT',
      total: 'Total',
      teamRun: 'Cross-machine',
    }
  }
  return {
    speed: 'Generation speed',
    prompt: 'Prompt processing',
    firstResponse: 'First response',
    total: 'Total response time',
    teamRun: 'Team run',
  }
}

export function estimateBenchmarkMinutes(
  modelCount: number,
  promptCount: number,
  runsPerPrompt: number,
): number {
  if (modelCount < 1 || promptCount < 1) return 0
  const steps = modelCount * promptCount * (1 + Math.max(1, runsPerPrompt))
  // ~10–14s per measured step including load/unload overhead
  return Math.max(1, Math.round((steps * 12) / 60))
}

export function memoryUsePercent(total?: number, available?: number): number | null {
  if (!total || total <= 0 || available == null) return null
  const used = total - available
  if (used < 0) return null
  return Math.min(100, Math.round((used / total) * 100))
}

export function stepMetricRows(step: GenerationRoleStep, advanced: boolean) {
  const labels = metricLabels(advanced)
  return [
    { label: labels.firstResponse, value: formatMs(step.ttft_ms) },
    { label: labels.prompt, value: formatMs(step.prompt_ms) },
    { label: labels.total, value: formatMs(step.total_ms) },
    {
      label: labels.speed,
      value: step.eval_tok_per_sec > 0 ? `${formatRate(step.eval_tok_per_sec)} tok/s` : '—',
    },
  ]
}
