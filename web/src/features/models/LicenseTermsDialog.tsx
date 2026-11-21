import { useTranslation } from 'react-i18next'
import { useDialog } from '@/lib/useDialog'
import type { ModelLicense } from '@/types/api'

/** A model's license terms, read and accepted before it's installed (#593). */
export function LicenseTermsDialog({
  modelName,
  license,
  onCancel,
  onAccept,
}: {
  modelName: string
  license: ModelLicense
  onCancel: () => void
  onAccept: () => void
}) {
  const { t } = useTranslation('models')
  const ref = useDialog(true, onCancel)
  return (
    <div className="fixed inset-0 z-50 flex items-end justify-center scrim p-4 sm:items-center" role="presentation" onClick={onCancel}>
      <div
        ref={ref}
        className="card w-full max-w-md space-y-4 shadow-panel"
        role="dialog"
        aria-modal="true"
        aria-labelledby="license-terms-title"
        onClick={(event) => event.stopPropagation()}
      >
        <h2 id="license-terms-title" className="font-display text-lg font-semibold text-ink">
          {t('licenseTerms.title', { model: modelName })}
        </h2>
        <p className="text-sm text-ink-muted">{t('licenseTerms.body', { model: modelName, license: license.name })}</p>
        {license.notes ? <p className="text-sm text-ink">{license.notes}</p> : null}
        <a className="btn-secondary btn-sm inline-flex" href={license.url} target="_blank" rel="noreferrer noopener">
          {t('licenseTerms.read', { license: license.name })}
        </a>
        <p className="text-xs text-ink-faint">{t('licenseTerms.record')}</p>
        <div className="flex flex-wrap justify-end gap-2">
          <button type="button" className="btn-secondary" data-autofocus onClick={onCancel}>
            {t('licenseTerms.cancel')}
          </button>
          <button type="button" className="btn-primary" onClick={onAccept}>
            {t('licenseTerms.accept')}
          </button>
        </div>
      </div>
    </div>
  )
}
