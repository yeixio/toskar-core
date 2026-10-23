import type { FitLabel, LanguageCapability, Model, ModelFit } from '@/types/api'
import i18n from '@/i18n'
import { formatDate, formatLocale, formatNumber, formatRelativeTime } from '@/i18n/format'
import { formatBytes } from '@/lib/format'


// Each section's title is models:categories.<id> in the catalog.
export const CATEGORY_SECTIONS: { id: string; match: (m: Model) => boolean }[] = [
  {
    id: 'coding',
    match: (m) => Boolean(m.capabilities?.coding || m.tags?.includes('coding')),
  },
  {
    id: 'general',
    match: (m) =>
      Boolean(m.tags?.includes('general') || m.purpose?.includes('general') || m.purpose?.includes('assistant')),
  },
  {
    id: 'reasoning',
    match: (m) => Boolean(m.tags?.includes('reasoning')),
  },
  {
    id: 'vision',
    match: (m) => Boolean(m.capabilities?.vision || m.tags?.includes('vision')),
  },
  {
    id: 'fast',
    match: (m) => Boolean(m.tags?.includes('fast')),
  },
  {
    id: 'large',
    match: (m) => Boolean(m.tags?.includes('large')),
  },
]

/** The fit chip's colours: green when it runs well, amber when tight, red when it won't. */
export function fitTone(label?: string): string {
  switch (label) {
    case 'excellent':
    case 'good':
      return 'bg-success/15 text-success'
    case 'tight':
      return 'bg-warning/15 text-warning'
    case 'heavy':
    case 'too_large':
    case 'unsupported':
      return 'bg-danger/15 text-danger'
    default:
      return 'bg-raised text-ink-muted'
  }
}

export function fitLabelText(label?: FitLabel): string {
  switch (label) {
    case 'excellent':
      return i18n.t('models:fit.excellent')
    case 'good':
      return i18n.t('models:fit.good')
    case 'tight':
      return i18n.t('models:fit.tight')
    case 'heavy':
    case 'too_large':
      return i18n.t('models:fit.heavy')
    case 'unsupported':
      return i18n.t('models:fit.unsupported')
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
    return { label: i18n.t('models:install.unsupported'), disabled: true }
  }
  if (fit?.label === 'heavy' || fit?.label === 'too_large') {
    return { label: i18n.t('models:install.anyway'), disabled: false }
  }
  return { label: i18n.t('models:install.install'), disabled: false }
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
  if (tokens % 1024 === 0) return `${formatNumber(tokens / 1024)}K`
  return formatNumber(tokens)
}

export function speedLabel(fit?: ModelFit): string {
  if (!fit?.est_tok_per_sec) return ''
  const rounded = formatNumber(fit.est_tok_per_sec, { maximumFractionDigits: fit.tok_per_sec_measured ? 1 : 0 })
  return i18n.t(fit.tok_per_sec_measured ? 'models:speed.measured' : 'models:speed.estimated', { value: rounded })
}

export function runtimeRangeLabel(fit?: ModelFit): string {
  const low = fit?.runtime_memory_low_bytes
  const high = fit?.runtime_memory_high_bytes
  if (!low) return ''
  const range = !high || high === low ? formatBytes(low) : `${formatBytes(low)}–${formatBytes(high)}`
  return fit?.approximate ? i18n.t('models:memory.approximate', { range }) : range
}

export function machineMemoryLabel(fit?: ModelFit): string {
  if (!fit?.total_memory_bytes) return ''
  const size = formatBytes(fit.total_memory_bytes)
  return i18n.t(fit.memory_kind === 'unified' ? 'models:memory.unified' : 'models:memory.plain', { size })
}

export function statusLabel(model: Model, runningIds: Set<string>): string {
  if (runningIds.has(model.id)) return i18n.t('models:status.running')
  if (model.status === 'downloading') return i18n.t('models:status.downloading')
  if (model.installed) return i18n.t('models:status.installed')
  return i18n.t('models:status.notInstalled')
}

export function memoryBarPercent(fit?: ModelFit, totalMem?: number): number {
  if (!fit?.expected_memory_bytes || !totalMem) return 0
  return Math.min(100, Math.round((fit.expected_memory_bytes / totalMem) * 100))
}

export function formatLastUsed(iso?: string): string {
  if (!iso) return i18n.t('models:lastUsed.never')
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return i18n.t('models:lastUsed.never')
  const mins = Math.round((Date.now() - d.getTime()) / 60000)
  if (mins < 1) return i18n.t('models:lastUsed.justNow')
  if (mins < 48 * 60) return formatRelativeTime(d)
  return formatDate(d)
}

export function modelTags(model: Model): string[] {
  const tags = [...(model.tags ?? [])]
  if (model.capabilities?.tool_calling && !tags.includes('tools')) tags.push('tools')
  if (model.context && model.context >= 32000 && !tags.some((t) => t.includes('context'))) {
    tags.push(i18n.t('models:tags.context', { count: Math.round(model.context / 1000) }))
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
    return { summary: i18n.t('models:tools.webSummary'), detail: i18n.t('models:tools.webDetail') }
  }
  if (usesTools) {
    return { summary: i18n.t('models:tools.blockedSummary'), detail: i18n.t('models:tools.blockedDetail') }
  }
  return { summary: i18n.t('models:tools.chatSummary'), detail: i18n.t('models:tools.chatDetail') }
}

export type PurposeChip = 'coding' | 'tools' | 'reasoning' | 'vision' | 'fast' | 'general'

/** What a model is for, as ids: coding, tools, reasoning, vision, fast, or general. */
export function purposeChipIds(model: Model): PurposeChip[] {
  const chips: PurposeChip[] = []
  if (model.capabilities?.coding || model.tags?.includes('coding')) chips.push('coding')
  if (model.capabilities?.tool_calling || model.tags?.includes('tools')) chips.push('tools')
  if (model.tags?.includes('reasoning')) chips.push('reasoning')
  if (model.capabilities?.vision || model.tags?.includes('vision')) chips.push('vision')
  if (model.tags?.includes('fast')) chips.push('fast')
  if (chips.length === 0) chips.push('general')
  return chips.slice(0, 4)
}

/** Lay-user capability chips (no GGUF / quantization jargon), in the App language. */
export function purposeChips(model: Model): string[] {
  return purposeChipIds(model).map((chip) => i18n.t(`models:chips.${chip}`))
}

/** True for a model that can answer a chat. */
export function canChat(model: Pick<Model, 'support_role'>): boolean {
  return !model.support_role
}

export function bestForLabel(model: Model): string {
  if (model.support_role) {
    return i18n.t(`models:support.${model.support_role}`)
  }
  if (model.capabilities?.coding || model.tags?.includes('coding')) {
    return i18n.t('models:bestFor.coding')
  }
  if (model.tags?.includes('reasoning')) {
    return i18n.t('models:bestFor.reasoning')
  }
  if (model.capabilities?.vision || model.tags?.includes('vision')) {
    return i18n.t('models:bestFor.vision')
  }
  if (model.tags?.includes('fast')) {
    return i18n.t('models:bestFor.fast')
  }
  if (model.tags?.includes('large')) {
    return i18n.t('models:bestFor.large')
  }
  const first = model.summary?.split(/[.!?]/)[0]?.trim()
  if (first && first.length < 60) return first
  return i18n.t('models:bestFor.everyday')
}

export function downloadLabel(model: Model): string {
  if (model.size_bytes) return formatBytes(model.size_bytes)
  return '—'
}

export function memoryLabel(model: Model): string {
  if (model.memory_needed_bytes) return i18n.t('models:memory.needed', { size: formatBytes(model.memory_needed_bytes) })
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

/** A language's name in the App language, such as Deutsch or German. */
export function languageName(tag: string): string {
  try {
    return new Intl.DisplayNames([formatLocale()], { type: 'language' }).of(tag) ?? tag
  } catch {
    return tag
  }
}

/**
 * The model's level for a language: the same tag, or the same language in
 * another region (pt-BR for pt). Chinese matches only in the same script.
 */
export function languageLevelFor(model: Pick<Model, 'languages'>, tag: string): LanguageCapability | undefined {
  const langs = model.languages ?? []
  const exact = langs.find((l) => l.language.toLowerCase() === tag.toLowerCase())
  if (exact) return exact
  const base = tag.split('-')[0].toLowerCase()
  if (base === 'zh') {
    const script = /hant|tw|hk|mo/i.test(tag) ? 'hant' : 'hans'
    return langs.find((l) => l.language.toLowerCase() === `zh-${script}`)
  }
  return langs.find((l) => l.language.split('-')[0].toLowerCase() === base)
}

/** One language's level, confidence, and sources, in words. */
export function languageDetail(l: LanguageCapability): string {
  return i18n.t('models:languages.detail', {
    level: i18n.t(`models:languages.levels.${l.level}`),
    confidence: i18n.t(`models:languages.confidence.${l.confidence}`),
    sources: l.sources.map((s) => i18n.t(`models:languages.sources.${s}`, { defaultValue: s })).join(', '),
  })
}

/**
 * What a model card says about languages: the model's level in the App
 * language, and how many others it has one for. title lists them all.
 */
export function languageSummary(model: Pick<Model, 'languages'>, appLanguage: string): { text: string; title: string } | null {
  const langs = model.languages ?? []
  if (langs.length === 0) return null
  const tag = appLanguage.startsWith('en-X') || appLanguage.startsWith('ar-X') ? 'en' : appLanguage
  const yours = languageLevelFor(model, tag)
  const title = langs.map((l) => `${languageName(l.language)}: ${languageDetail(l)}`).join('\n')
  const others = langs.filter((l) => l !== yours).length
  const head = yours
    ? i18n.t('models:languages.yours', { language: languageName(yours.language), level: i18n.t(`models:languages.levels.${yours.level}`) })
    : i18n.t('models:languages.notRated', { language: languageName(tag) })
  const text = others > 0 ? i18n.t('models:languages.withMore', { first: head, count: others }) : head
  return { text, title }
}
