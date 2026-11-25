import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { api } from '@/lib/api'
import { useRole } from '@/lib/role'
import { isDesktopShell } from '@/lib/desktopBridge'

/**
 * Schedules run only while Toskar runs. Toskar no longer turns
 * "Keep running in background" back on by itself (#1209), so when an Admin
 * has turned it off in the desktop app, say what that means here.
 */
export function BackgroundOffNotice() {
  const { t } = useTranslation('automations')
  const admin = useRole().atLeast('admin') && isDesktopShell()
  const settings = useQuery({
    queryKey: ['settings'],
    queryFn: () => api.getSettings(),
    enabled: admin,
    retry: false,
  })
  if (!admin || settings.data?.keep_running_in_background !== false) return null
  return (
    <p className="card text-sm text-ink-muted" role="note">
      {t('page.backgroundOff')}{' '}
      <Link className="font-medium text-primary hover:underline" to="/settings/general">
        {t('page.backgroundOffLink')}
      </Link>
    </p>
  )
}
