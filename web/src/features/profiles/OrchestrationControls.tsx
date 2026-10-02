import type { OrchestrationPolicy } from '@/types/api'

type Choice = { value: string; label: string }

const DEFAULT: Choice = { value: '', label: 'Default' }

const SELECTS: { key: keyof OrchestrationPolicy; label: string; help: string; options: Choice[] }[] = [
  {
    key: 'effort',
    label: 'Reasoning level',
    help: 'Used when a chat leaves effort on Auto.',
    options: [DEFAULT, { value: 'fast', label: 'Fast' }, { value: 'balanced', label: 'Balanced' }, { value: 'thorough', label: 'Thorough' }],
  },
  {
    key: 'planning',
    label: 'Planning',
    help: 'Work through requests with several parts in parts. Always also splits requests with no obvious parts.',
    options: [
      DEFAULT,
      { value: 'on', label: 'When a request has parts' },
      { value: 'always', label: 'Always' },
      { value: 'off', label: 'Never plan' },
    ],
  },
  {
    key: 'parallel',
    label: 'Parallelism',
    help: 'Look up independent parts side by side.',
    options: [DEFAULT, { value: 'on', label: 'Side by side' }, { value: 'off', label: 'One at a time' }],
  },
  {
    key: 'verification',
    label: 'Verification',
    help: 'Check figures against the sources before answering.',
    options: [
      DEFAULT,
      { value: 'off', label: 'Off' },
      { value: 'check', label: 'Check and report' },
      { value: 'correct', label: 'Check and correct once' },
      { value: 'thorough', label: 'Check and correct twice' },
    ],
  },
  {
    key: 'memory',
    label: 'Memory',
    help: 'Use persistent memories in this profile’s chats.',
    options: [DEFAULT, { value: 'off', label: 'Off for this profile' }],
  },
  {
    key: 'fallback',
    label: 'Fallback',
    help: 'Answer on another model when the chosen one fails.',
    options: [DEFAULT, { value: 'off', label: 'Show the failure instead' }],
  },
]

const NUMBERS: { key: keyof OrchestrationPolicy; label: string; help: string; min: number; max: number; step?: number }[] = [
  { key: 'max_workers', label: 'Workers', help: 'Most parts in a plan (2–8).', min: 2, max: 8 },
  { key: 'max_tool_calls', label: 'Tool calls', help: 'Most tool calls in one turn (1–50).', min: 1, max: 50 },
  { key: 'timeout_seconds', label: 'Time limit (s)', help: 'Stop a turn that runs longer (10–3600).', min: 10, max: 3600 },
  { key: 'context_share', label: 'Context budget', help: 'Most of the window earlier messages may use (0.1–0.9).', min: 0.1, max: 0.9, step: 0.05 },
]

/** Drops empty controls, so a profile keeps every default it does not change. */
export function cleanOrchestration(o: OrchestrationPolicy): OrchestrationPolicy | undefined {
  const out: Record<string, unknown> = {}
  for (const [k, v] of Object.entries(o)) {
    if (Array.isArray(v) && v.length === 0) continue
    if (v !== '' && v !== undefined && v !== 0 && !(typeof v === 'number' && Number.isNaN(v))) out[k] = v
  }
  return Object.keys(out).length > 0 ? (out as OrchestrationPolicy) : undefined
}

/**
 * A profile's orchestration controls (spec §40): reasoning level, planning,
 * workers, parallelism, verification, tool calls, memory, context budget,
 * fallback, and time limit. Blank means Yggdrasil's default.
 */
export function OrchestrationControls({
  value,
  onChange,
  disabled,
}: {
  value: OrchestrationPolicy
  onChange: (next: OrchestrationPolicy) => void
  disabled?: boolean
}) {
  const set = (patch: OrchestrationPolicy) => onChange({ ...value, ...patch })
  return (
    <section className="space-y-3">
      <div>
        <h3 className="text-xs font-semibold uppercase tracking-wide text-ink-muted">Orchestration</h3>
        <p className="text-xs text-ink-muted">
          How this profile works through a request. Default follows the effort each chat chooses.
        </p>
      </div>
      <div className="grid gap-3 sm:grid-cols-2">
        {SELECTS.map((s) => (
          <label key={s.key} className="block text-sm" title={s.help}>
            <span className="text-ink-muted">{s.label}</span>
            <select
              className="field mt-1 w-full py-1 text-sm"
              value={(value[s.key] as string | undefined) ?? ''}
              disabled={disabled}
              onChange={(e) => set({ [s.key]: e.target.value } as OrchestrationPolicy)}
            >
              {s.options.map((o) => (
                <option key={o.value} value={o.value}>
                  {o.label}
                </option>
              ))}
            </select>
            <span className="mt-0.5 block text-xs text-ink-faint">{s.help}</span>
          </label>
        ))}
        {NUMBERS.map((n) => (
          <label key={n.key} className="block text-sm" title={n.help}>
            <span className="text-ink-muted">{n.label}</span>
            <input
              className="field mt-1 w-full py-1 text-sm"
              type="number"
              min={n.min}
              max={n.max}
              step={n.step ?? 1}
              placeholder="Default"
              value={(value[n.key] as number | undefined) || ''}
              disabled={disabled}
              onChange={(e) => set({ [n.key]: e.target.value === '' ? 0 : Number(e.target.value) } as OrchestrationPolicy)}
            />
            <span className="mt-0.5 block text-xs text-ink-faint">{n.help}</span>
          </label>
        ))}
      </div>
    </section>
  )
}
