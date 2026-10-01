export type ContextUsage = {
  promptTokens: number
  limit: number
  instructions: number
  tools: number
  conversation: number
  toolResults: number
  estimated: boolean
  /** Older saved messages the model saw as a summary. */
  summarizedMessages?: number
}

/** Local llama.cpp starts at this window unless the model advertises a smaller one. */
export const DEFAULT_CONTEXT_WINDOW = 8192

export function contextWindow(catalog?: number): number {
  if (!catalog || catalog <= 0 || catalog > DEFAULT_CONTEXT_WINDOW) {
    return DEFAULT_CONTEXT_WINDOW
  }
  return catalog
}

export function parseContextUsage(raw: unknown): ContextUsage | null {
  if (!raw || typeof raw !== 'object') return null
  const record = raw as Record<string, unknown>
  const usage: ContextUsage = {
    promptTokens: numberField(record.prompt_tokens),
    limit: numberField(record.limit),
    instructions: numberField(record.instructions),
    tools: numberField(record.tools),
    conversation: numberField(record.conversation),
    toolResults: numberField(record.tool_results),
    estimated: record.estimated === true,
    summarizedMessages: numberField(record.summarized_messages),
  }
  if (usage.promptTokens <= 0 && usage.instructions + usage.tools + usage.conversation + usage.toolResults <= 0) {
    return null
  }
  return usage
}

function numberField(value: unknown): number {
  const n = typeof value === 'number' ? value : Number(value)
  if (!Number.isFinite(n) || n < 0) return 0
  return Math.round(n)
}

export function formatTokens(n: number): string {
  if (!Number.isFinite(n) || n < 1000) return String(Math.max(0, Math.round(n) || 0))
  const thousands = n / 1000
  if (thousands >= 100) return `${Math.round(thousands)}K`
  const text = thousands.toFixed(1)
  return `${text.endsWith('.0') ? text.slice(0, -2) : text}K`
}

export function fillPercent(used: number, limit: number): number {
  if (limit <= 0 || used <= 0) return 0
  return Math.round((used / limit) * 100)
}

export type ContextRow = {
  id: 'instructions' | 'tools' | 'conversation' | 'toolResults'
  label: string
  tokens: number
}

export function contextRows(usage: ContextUsage): ContextRow[] {
  const rows: ContextRow[] = [
    { id: 'instructions', label: 'Instructions', tokens: usage.instructions },
    { id: 'tools', label: 'Tools', tokens: usage.tools },
    { id: 'conversation', label: 'Conversation', tokens: usage.conversation },
    { id: 'toolResults', label: 'Tool results', tokens: usage.toolResults },
  ]
  return rows.filter((row) => row.tokens > 0)
}
