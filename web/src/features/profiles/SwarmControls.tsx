import { useTranslation } from 'react-i18next'
import type { SwarmPolicy } from '@/types/api'
import { MAX_MODELS, SWARM_NUMBERS as NUMBERS } from './orchestrationKeys'

/**
 * A profile's swarm controls (#1135), shown when its strategy is Swarm:
 * Split or Race, the models in order, and the budgets. Blank keeps Core's
 * default.
 */
export function SwarmControls({
  value,
  onChange,
  installedModels,
  disabled,
}: {
  value: SwarmPolicy
  onChange: (next: SwarmPolicy) => void
  installedModels: { id: string; name: string }[]
  disabled?: boolean
}) {
  const { t } = useTranslation('profiles')
  const set = (patch: SwarmPolicy) => onChange({ ...value, ...patch })
  const mode = value.mode || 'split'
  const models = value.models ?? []
  const nameOf = (id: string) => installedModels.find((m) => m.id === id)?.name || id
  const addable = installedModels.filter((m) => !models.includes(m.id))
  const move = (index: number, by: number) => {
    const next = [...models]
    const [item] = next.splice(index, 1)
    next.splice(index + by, 0, item)
    set({ models: next })
  }

  return (
    <fieldset className="space-y-3 rounded-lg border border-line/60 p-3" disabled={disabled}>
      <legend className="px-1 text-sm font-semibold text-ink">{t('swarm.title')}</legend>
      <label className="block text-sm" title={t('swarm.mode.help')}>
        <span className="text-ink-muted">{t('swarm.mode.label')}</span>
        <select
          className="field mt-1 w-full py-1 text-sm"
          value={mode}
          onChange={(e) => set({ mode: e.target.value as SwarmPolicy['mode'] })}
        >
          <option value="split">{t('swarm.mode.split')}</option>
          <option value="race">{t('swarm.mode.race')}</option>
        </select>
        <span className="mt-0.5 block text-xs text-ink-faint">{t('swarm.mode.help')}</span>
      </label>

      <div className="text-sm">
        <span className="text-ink-muted" id="swarm-models-label">
          {t('swarm.models.label')}
        </span>
        {models.length > 0 ? (
          <ol className="mt-1 space-y-1" aria-labelledby="swarm-models-label">
            {models.map((id, i) => (
              <li key={id} className="flex items-center gap-2 rounded-md bg-raised/60 px-2 py-1">
                <span className="w-4 shrink-0 text-xs text-ink-faint">{i + 1}.</span>
                <span className="min-w-0 flex-1 truncate text-ink">{nameOf(id)}</span>
                <button
                  type="button"
                  className="btn-secondary btn-sm"
                  disabled={i === 0}
                  aria-label={t('swarm.models.up', { model: nameOf(id) })}
                  onClick={() => move(i, -1)}
                >
                  <span aria-hidden>↑</span>
                </button>
                <button
                  type="button"
                  className="btn-secondary btn-sm"
                  aria-label={t('swarm.models.remove', { model: nameOf(id) })}
                  onClick={() => set({ models: models.filter((m) => m !== id) })}
                >
                  <span aria-hidden>×</span>
                </button>
              </li>
            ))}
          </ol>
        ) : null}
        {models.length < MAX_MODELS && addable.length > 0 ? (
          <select
            className="field mt-1 w-full py-1 text-sm"
            aria-label={t('swarm.models.add')}
            value=""
            onChange={(e) => e.target.value && set({ models: [...models, e.target.value] })}
          >
            <option value="">{t('swarm.models.add')}</option>
            {addable.map((m) => (
              <option key={m.id} value={m.id}>
                {m.name || m.id}
              </option>
            ))}
          </select>
        ) : null}
        <span className="mt-0.5 block text-xs text-ink-faint">{t('swarm.models.help')}</span>
        {mode === 'race' && models.length < 2 ? (
          <span className="mt-0.5 block text-xs text-warning" role="status">
            {t('swarm.models.raceNeedsTwo')}
          </span>
        ) : null}
      </div>

      <div className="grid gap-3 sm:grid-cols-3">
        {NUMBERS.map((n) => (
          <label key={n.key} className="block text-sm" title={t(`swarm.${n.key}.help`)}>
            <span className="text-ink-muted">{t(`swarm.${n.key}.label`)}</span>
            <input
              className="field mt-1 w-full py-1 text-sm"
              type="number"
              min={n.min}
              max={n.max}
              step={n.step ?? 1}
              placeholder={t('controls.default')}
              value={value[n.key] || ''}
              onChange={(e) => set({ [n.key]: e.target.value === '' ? 0 : Number(e.target.value) })}
            />
            <span className="mt-0.5 block text-xs text-ink-faint">{t(`swarm.${n.key}.help`)}</span>
          </label>
        ))}
      </div>
    </fieldset>
  )
}
