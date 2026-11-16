export type ModelFailure = {
  kind: 'model_health'
  reason: string
  message: string
  likely_memory_pressure?: boolean
  interrupted?: boolean
  model_id?: string
  running_model_id?: string
  node_id?: string
  runtime?: string
  exit_code?: number
  last_probe?: string
  stderr_tail?: string
  /** The model kept running that holds the memory this one needed (#966). */
  kept_running_model_id?: string
  kept_running_name?: string
}

export function parseModelFailure(raw: string): ModelFailure | null {
  const text = raw.trim()
  if (!text.startsWith('{')) return null
  try {
    const parsed = JSON.parse(text) as Partial<ModelFailure>
    if (parsed.kind !== 'model_health' || !parsed.message || !parsed.reason) return null
    return parsed as ModelFailure
  } catch {
    return null
  }
}
