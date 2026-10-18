import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { formatNumber } from '@/i18n/format'
import type { NetworkAccess } from '@/types/api'

/**
 * Who connects from another computer, and how (#711): people sign in with
 * their username and password, phones and tablets with a code, and apps and
 * scripts with an API key. A key is for apps only; people never need one.
 */
export function WhoConnects({ access, onConnectDevice }: { access?: NetworkAccess; onConnectDevice: () => void }) {
  const { t } = useTranslation('apiAccess')
  const count = (n: number) => ({ count: n, formatted: formatNumber(n) })
  return (
    <dl className="grid gap-3 text-sm sm:grid-cols-3">
      <div className="rounded-lg border border-line px-3 py-2">
        <dt className="font-medium text-ink">{t('lan.who.people')}</dt>
        <dd className="mt-1 text-ink-muted">{t('lan.who.peopleHow')}</dd>
        <dd className="mt-1 text-xs text-ink-faint">
          {access ? t('lan.who.peopleCount', count(access.people)) : null}
          {access?.sign_in_service ? ` · ${t('lan.who.signInService')}` : null}{' '}
          <Link to="/people" className="text-primary hover:underline">
            {t('lan.who.peopleAction')}
          </Link>
        </dd>
      </div>
      <div className="rounded-lg border border-line px-3 py-2">
        <dt className="font-medium text-ink">{t('lan.who.devices')}</dt>
        <dd className="mt-1 text-ink-muted">{t('lan.who.devicesHow')}</dd>
        <dd className="mt-1 text-xs text-ink-faint">
          {access ? t('lan.who.devicesCount', count(access.devices)) : null}{' '}
          <button type="button" className="text-primary hover:underline" onClick={onConnectDevice}>
            {t('lan.who.devicesAction')}
          </button>
        </dd>
      </div>
      <div className="rounded-lg border border-line px-3 py-2">
        <dt className="font-medium text-ink">{t('lan.who.apps')}</dt>
        <dd className="mt-1 text-ink-muted">{t('lan.who.appsHow')}</dd>
        {access ? <dd className="mt-1 text-xs text-ink-faint">{t('lan.who.appsCount', count(access.keys))}</dd> : null}
      </div>
    </dl>
  )
}
