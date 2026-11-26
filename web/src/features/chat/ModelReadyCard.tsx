import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { formatSize } from '@/i18n/format'
import type { MainModelView } from '@/types/api'

/**
 * Asks an Admin how the main chat model stays ready (#1472): kept running, so
 * answers start at once, or managed by Toskar, which frees the memory when it
 * needs to and then waits for the model to load. It stays at the top of chat
 * until they choose; Settings › AI behavior has the same choice.
 */
export function ModelReadyCard({
  model,
  busy,
  onChoose,
}: {
  model: MainModelView
  busy?: boolean
  onChoose: (choice: 'always' | 'managed') => void
}) {
  const { t } = useTranslation('chat')
  const size = formatSize(model.memory_bytes)
  const always =
    model.memory_total_bytes > 0
      ? t('modelReady.alwaysHintOf', { size, total: formatSize(model.memory_total_bytes) })
      : t('modelReady.alwaysHint', { size })
  const managed = model.fits ? t('modelReady.managedHintFits') : t('modelReady.managedHintBig')
  return (
    <section
      aria-labelledby="model-ready-title"
      className="mx-auto mt-2 w-full max-w-3xl shrink-0 rounded-xl border border-primary/30 bg-primary-soft/40 px-4 py-3 text-sm"
    >
      <h2 id="model-ready-title" className="font-medium text-ink">
        {t('modelReady.title', { model: model.name })}
      </h2>
      <p className="mt-1 text-ink-muted">{t('modelReady.body')}</p>
      <div className="mt-3 grid gap-2 sm:grid-cols-2">
        <button type="button" className="btn-secondary h-auto flex-col items-start gap-0.5 py-2 text-start" disabled={busy} onClick={() => onChoose('always')}>
          <span className="font-medium text-ink">{t('modelReady.always')}</span>
          <span className="text-xs font-normal text-ink-muted">{always}</span>
        </button>
        <button type="button" className="btn-secondary h-auto flex-col items-start gap-0.5 py-2 text-start" disabled={busy} onClick={() => onChoose('managed')}>
          <span className="font-medium text-ink">{t('modelReady.managed')}</span>
          <span className="text-xs font-normal text-ink-muted">{managed}</span>
        </button>
      </div>
      <p className="mt-2 text-xs text-ink-faint">
        {t('modelReady.later')}{' '}
        <Link to="/settings/models#model-lifecycle" className="text-primary underline-offset-2 hover:underline">
          {t('modelReady.settings')}
        </Link>
      </p>
    </section>
  )
}
