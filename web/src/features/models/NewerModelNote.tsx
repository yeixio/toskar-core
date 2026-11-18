import { useTranslation } from 'react-i18next'
import type { Model, ModelFit } from '@/types/api'
import { fitLabelText } from './modelPresentation'

/**
 * For an installed model with a newer one in its line (#593): which, how it
 * fits this computer, whether its license differs, and a button that
 * installs it beside this one. Nothing changes until the person installs
 * it, and profiles keep the model they name.
 */
export function NewerModelNote({
  model,
  models,
  fits,
  onInstall,
  installingId,
}: {
  model: Model
  models: Model[]
  fits?: Record<string, ModelFit>
  onInstall?: (id: string) => void
  installingId?: string | null
}) {
  const { t } = useTranslation('models')
  const next = model.newer ? models.find((m) => m.id === model.newer) : undefined
  if (!next || next.installed) return null
  const fit = fitLabelText((fits?.[next.id] ?? next.fit)?.label)
  const licenseChanged = next.license && model.license && next.license.id !== model.license.id
  return (
    <div role="note" className="mt-2 rounded-lg bg-primary/10 px-2.5 py-2 text-xs leading-relaxed text-ink">
      <p className="font-medium">{t('newer.title', { model: next.display_name })}</p>
      <p className="mt-1 text-ink-muted">
        {fit ? t('newer.fit', { fit }) : null}
        {fit && licenseChanged ? ' ' : null}
        {licenseChanged ? t('newer.license', { license: next.license!.name }) : null}
      </p>
      {onInstall ? (
        <p className="mt-1 text-ink-muted">
          <button
            type="button"
            className="font-medium text-primary underline-offset-2 hover:underline disabled:opacity-60"
            disabled={installingId === next.id}
            onClick={() => onInstall(next.id)}
          >
            {t('newer.install', { model: next.display_name })}
          </button>{' '}
          {t('newer.beside')}
        </p>
      ) : null}
    </div>
  )
}
