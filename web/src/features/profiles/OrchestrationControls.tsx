import { useTranslation } from 'react-i18next'
import type { OrchestrationPolicy } from '@/types/api'
import { cleanSwarm } from './orchestrationKeys'

// Each control's label and help, and each option's name, are
// profiles:controls.<key> in the catalog; '' is the default.
const SELECTS: { key: keyof OrchestrationPolicy; options: string[] }[] = [
  { key: 'effort', options: ['', 'fast', 'balanced', 'thorough'] },
  { key: 'planning', options: ['', 'on', 'always', 'off'] },
  { key: 'parallel', options: ['', 'on', 'off'] },
  { key: 'verification', options: ['', 'off', 'check', 'correct', 'thorough'] },
  { key: 'memory', options: ['', 'off'] },
  { key: 'fallback', options: ['', 'off'] },
  // Auto joins when the quality run sets its rule (#459).
  { key: 'deliberate', options: ['', 'always'] },
  { key: 'media_quality', options: ['', 'fast', 'balanced', 'best'] },
]

const NUMBERS: { key: keyof OrchestrationPolicy; min: number; max: number; step?: number }[] = [
  { key: 'max_workers', min: 2, max: 8 },
  { key: 'max_tool_calls', min: 1, max: 50 },
  { key: 'retries', min: 1, max: 3 },
  { key: 'timeout_seconds', min: 10, max: 3600 },
  { key: 'context_share', min: 0.1, max: 0.9, step: 0.05 },
]

/** Drops empty controls, so a profile keeps every default it does not change. */
export function cleanOrchestration(o: OrchestrationPolicy): OrchestrationPolicy | undefined {
  const out: Record<string, unknown> = {}
  for (const [k, v] of Object.entries(o)) {
    if (k === 'swarm') {
      const swarm = cleanSwarm(o.swarm)
      if (swarm) out.swarm = swarm
      continue
    }
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
  only,
}: {
  value: OrchestrationPolicy
  onChange: (next: OrchestrationPolicy) => void
  disabled?: boolean
  /** The controls to show; all of them when omitted. */
  only?: (keyof OrchestrationPolicy)[]
}) {
  const { t } = useTranslation('profiles')
  const set = (patch: OrchestrationPolicy) => onChange({ ...value, ...patch })
  const shown = (key: keyof OrchestrationPolicy) => !only || only.includes(key)
  return (
    <div className={only?.length === 1 ? 'grid gap-3' : 'grid gap-3 sm:grid-cols-2'}>
        {SELECTS.filter((s) => shown(s.key)).map((s) => (
          <label key={s.key} className="block text-sm" title={t(`controls.${s.key}.help`)}>
            <span className="text-ink-muted">{t(`controls.${s.key}.label`)}</span>
            <select
              className="field mt-1 w-full py-1 text-sm"
              value={(value[s.key] as string | undefined) ?? ''}
              disabled={disabled}
              onChange={(e) => set({ [s.key]: e.target.value } as OrchestrationPolicy)}
            >
              {s.options.map((o) => (
                <option key={o} value={o}>
                  {/* A control can say what its default is, such as Off. */}
                  {o ? t(`controls.${s.key}.${o}`) : t(`controls.${s.key}.default`, { defaultValue: t('controls.default') })}
                </option>
              ))}
            </select>
            <span className="mt-0.5 block text-xs text-ink-faint">{t(`controls.${s.key}.help`)}</span>
          </label>
        ))}
        {NUMBERS.filter((n) => shown(n.key)).map((n) => (
          <label key={n.key} className="block text-sm" title={t(`controls.${n.key}.help`)}>
            <span className="text-ink-muted">{t(`controls.${n.key}.label`)}</span>
            <input
              className="field mt-1 w-full py-1 text-sm"
              type="number"
              min={n.min}
              max={n.max}
              step={n.step ?? 1}
              placeholder={t('controls.default')}
              value={(value[n.key] as number | undefined) || ''}
              disabled={disabled}
              onChange={(e) => set({ [n.key]: e.target.value === '' ? 0 : Number(e.target.value) } as OrchestrationPolicy)}
            />
            <span className="mt-0.5 block text-xs text-ink-faint">{t(`controls.${n.key}.help`)}</span>
          </label>
        ))}
    </div>
  )
}

