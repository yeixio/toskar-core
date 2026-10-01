import type { FitLabel, Model, ModelFit } from '@/types/api'
import { formatBytes } from '@/lib/format'

export const CATEGORY_SECTIONS: { id: string; title: string; match: (m: Model) => boolean }[] = [
  {
    id: 'coding',
    title: 'Coding',
    match: (m) => Boolean(m.capabilities?.coding || m.tags?.includes('coding')),
  },
  {
    id: 'general',
    title: 'General assistants',
    match: (m) =>
      Boolean(m.tags?.includes('general') || m.purpose?.includes('general') || m.purpose?.includes('assistant')),
  },
  {
    id: 'reasoning',
    title: 'Reasoning',
    match: (m) => Boolean(m.tags?.includes('reasoning')),
  },
  {
    id: 'vision',
    title: 'Vision',
    match: (m) => Boolean(m.capabilities?.vision || m.tags?.includes('vision')),
  },
  {
    id: 'fast',
    title: 'Small & fast',
    match: (m) => Boolean(m.tags?.includes('fast')),
  },
  {
    id: 'large',
    title: 'Large models',
    match: (m) => Boolean(m.tags?.includes('large')),
  },
]

export function fitLabelText(label?: FitLabel): string {
  switch (label) {
    case 'excellent':
      return 'Excellent fit'
    case 'good':
      return 'Good fit'
    case 'tight':
      return 'Tight fit'
    case 'heavy':
    case 'too_large':
      return 'May run slowly'
    case 'unsupported':
      return 'Unsupported'
    default:
      return ''
  }
}

export function needsTightFitInstallWarning(
  model: { installed?: boolean },
  fit?: { label?: FitLabel } | null,
): boolean {
  if (model.installed) return false
  return fit?.label === 'tight'
}

export function installAction(fit?: ModelFit): { label: string; disabled: boolean } {
  if (fit?.install_allowed === false || fit?.label === 'unsupported') {
    return { label: 'Unsupported', disabled: true }
  }
  if (fit?.label === 'heavy' || fit?.label === 'too_large') {
    return { label: 'Install anyway', disabled: false }
  }
  return { label: 'Install', disabled: false }
}

const FIT_RANK: Record<FitLabel, number> = {
  excellent: 5,
  good: 4,
  tight: 3,
  heavy: 2,
  too_large: 2,
  unsupported: 1,
}

export function bestMachineName(
  fits: { nodeName: string; label: FitLabel }[],
): string {
  let best = ''
  let rank = 0
  for (const fit of fits) {
    const next = FIT_RANK[fit.label] ?? 0
    if (next > rank) {
      rank = next
      best = fit.nodeName
    }
  }
  return best
}

export function contextTokensLabel(tokens?: number): string {
  if (!tokens) return '—'
  if (tokens % 1024 === 0) return `${tokens / 1024}K`
  return tokens.toLocaleString()
}

export function speedLabel(fit?: ModelFit): string {
  if (!fit?.est_tok_per_sec) return ''
  const rounded = fit.tok_per_sec_measured
    ? fit.est_tok_per_sec.toFixed(1).replace(/\.0$/, '')
    : String(Math.round(fit.est_tok_per_sec))
  return fit.tok_per_sec_measured ? `Measured ${rounded} tok/s` : `Estimated ~${rounded} tok/s`
}

export function runtimeRangeLabel(fit?: ModelFit): string {
  const low = fit?.runtime_memory_low_bytes
  const high = fit?.runtime_memory_high_bytes
  if (!low) return ''
  const suffix = fit?.approximate ? ' approximate' : ''
  if (!high || high === low) return `${formatBytes(low)}${suffix}`
  return `${formatBytes(low)}–${formatBytes(high)}${suffix}`
}

export function machineMemoryLabel(fit?: ModelFit): string {
  if (!fit?.total_memory_bytes) return ''
  const size = formatBytes(fit.total_memory_bytes)
  return fit.memory_kind === 'unified' ? `${size} unified memory` : `${size} memory`
}

export function statusLabel(model: Model, runningIds: Set<string>): string {
  if (runningIds.has(model.id)) return 'Running'
  if (model.status === 'downloading') return 'Downloading'
  if (model.installed) return 'Installed'
  return 'Not installed'
}

export function memoryBarPercent(fit?: ModelFit, totalMem?: number): number {
  if (!fit?.expected_memory_bytes || !totalMem) return 0
  return Math.min(100, Math.round((fit.expected_memory_bytes / totalMem) * 100))
}

export function formatLastUsed(iso?: string): string {
  if (!iso) return 'Never'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return 'Never'
  const mins = Math.round((Date.now() - d.getTime()) / 60000)
  if (mins < 1) return 'Just now'
  if (mins < 60) return `${mins} minute${mins === 1 ? '' : 's'} ago`
  const hours = Math.round(mins / 60)
  if (hours < 48) return `${hours} hour${hours === 1 ? '' : 's'} ago`
  return d.toLocaleDateString()
}

export function modelTags(model: Model): string[] {
  const tags = [...(model.tags ?? [])]
  if (model.capabilities?.tool_calling && !tags.includes('tools')) tags.push('tools')
  if (model.context && model.context >= 32000 && !tags.some((t) => t.includes('context'))) {
    tags.push(`${Math.round(model.context / 1000)}K context`)
  }
  return tags.slice(0, 4)
}

export type ModelToolAssessment = {
  summary: string
  detail: string
}

export function modelUsesTools(model: Pick<Model, 'capabilities' | 'tags'>): boolean {
  return Boolean(model.capabilities?.tool_calling || model.tags?.includes('tools'))
}

/**
 * A tool-calling model can fetch a page by running a terminal command such as curl.
 * That still waits for approval. Models without tool calling cannot reach the web.
 */
export function modelToolAssessment(
  model: Pick<Model, 'capabilities' | 'tags'>,
  options?: { terminalAllowed?: boolean },
): ModelToolAssessment {
  const usesTools = modelUsesTools(model)
  const terminalAllowed = options?.terminalAllowed !== false
  if (usesTools && terminalAllowed) {
    return {
      summary: 'Uses local tools · Can fetch the web',
      detail:
        'Can read files, use git, and run terminal commands after you approve them. A command such as curl can look something up on the web. There is no separate browser.',
    }
  }
  if (usesTools) {
    return {
      summary: 'Uses local tools · Web blocked by this profile',
      detail:
        'This model can run terminal commands, but the current assistant profile turns the terminal off, so it cannot fetch web pages.',
    }
  }
  return {
    summary: 'Chat only · No web access',
    detail:
      'Answers from the model itself. It cannot run a terminal command, so it cannot look anything up on the web.',
  }
}

/** Lay-user capability chips (no GGUF / quantization jargon). */
export function purposeChips(model: Model): string[] {
  const chips: string[] = []
  if (model.capabilities?.coding || model.tags?.includes('coding')) chips.push('Coding')
  if (model.capabilities?.tool_calling || model.tags?.includes('tools')) chips.push('Tools')
  if (model.tags?.includes('reasoning')) chips.push('Reasoning')
  if (model.capabilities?.vision || model.tags?.includes('vision')) chips.push('Vision')
  if (model.tags?.includes('fast')) chips.push('Fast')
  if (chips.length === 0) chips.push('General')
  return chips.slice(0, 4)
}

const SUPPORT_LABELS: Record<NonNullable<Model['support_role']>, string> = {
  embedding: 'Embedding model: helps search knowledge, does not chat',
  reranker: 'Reranker: orders search results, does not chat',
  classifier: 'Classifier: sorts requests, does not chat',
}

/** True for a model that can answer a chat. */
export function canChat(model: Pick<Model, 'support_role'>): boolean {
  return !model.support_role
}

export function bestForLabel(model: Model): string {
  if (model.support_role) {
    return SUPPORT_LABELS[model.support_role]
  }
  if (model.capabilities?.coding || model.tags?.includes('coding')) {
    return 'Best for programming'
  }
  if (model.tags?.includes('reasoning')) {
    return 'Best for careful reasoning'
  }
  if (model.capabilities?.vision || model.tags?.includes('vision')) {
    return 'Best for images and documents'
  }
  if (model.tags?.includes('fast')) {
    return 'Best for quick answers'
  }
  if (model.tags?.includes('large')) {
    return 'Best for hard problems'
  }
  const first = model.summary?.split(/[.!?]/)[0]?.trim()
  if (first && first.length < 60) return first
  return 'Everyday local assistant'
}

export function downloadLabel(model: Model): string {
  if (model.size_bytes) return formatBytes(model.size_bytes)
  return '—'
}

export function memoryLabel(model: Model): string {
  if (model.memory_needed_bytes) return `~${formatBytes(model.memory_needed_bytes)}`
  return '—'
}

/** Size in billions of parameters from a label such as "1B", "3.8B", or "500M". */
export function parameterBillions(model: Pick<Model, 'parameters'>): number | null {
  const p = model.parameters?.trim().toUpperCase() ?? ''
  const m = /^(\d+(?:\.\d+)?)\s*([BM])$/.exec(p)
  if (!m) return null
  const v = Number(m[1])
  return m[2] === 'M' ? v / 1000 : v
}

/** Small models (under 4B) are fast but more likely to mix up facts. Matches Huginn. */
export function isSmallModel(model: Pick<Model, 'parameters'>): boolean {
  const b = parameterBillions(model)
  return b != null && b < 4
}

export const SMALL_MODEL_NOTE =
  'Small and fast, but it can mix up facts and numbers, especially from your files and knowledge. For questions about your data, a larger model is more reliable.'

/**
 * A larger everyday model that fits this computer, to suggest next to a small
 * one. Installed models come first; otherwise the largest that fits well.
 */
export function largerAlternative(models: Model[], fits: Record<string, ModelFit>): Model | null {
  const everyday = (m: Model) =>
    !m.tags?.includes('vision') &&
    (m.purpose?.some((p) => p === 'general' || p === 'assistant') ?? true)
  const fitsWell = (m: Model) => ['excellent', 'good'].includes(fits[m.id]?.label ?? '')
  const candidates = models.filter((m) => !isSmallModel(m) && parameterBillions(m) != null && everyday(m))
  const size = (m: Model) => m.memory_needed_bytes ?? 0
  const installed = candidates.filter((m) => m.installed).sort((a, b) => size(b) - size(a))
  if (installed.length > 0) return installed[0]
  return candidates.filter(fitsWell).sort((a, b) => size(b) - size(a))[0] ?? null
}
