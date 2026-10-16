import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ConnectPhone } from '@/features/nodes/ConnectPhone'
import { formatDate } from '@/i18n/format'
import { api } from '@/lib/api'
import { LoadError } from '@/components/ui/LoadError'
import { Skeleton } from '@/components/ui/Skeleton'

/**
 * Settings → Your devices (#206): the phones, tablets, and TVs connected as
 * this person, whatever their role, with Connect a device and Disconnect.
 */
export function YourDevices() {
  const { t } = useTranslation('settings')
  const queryClient = useQueryClient()
  const [connecting, setConnecting] = useState(false)
  const devices = useQuery({ queryKey: ['my-devices'], queryFn: () => api.listMyDevices(), retry: false })
  const disconnect = useMutation({
    mutationFn: (id: string) => api.disconnectMyDevice(id),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ['my-devices'] }),
  })
  const list = devices.data ?? []

  return (
    <>
      <section className="card space-y-3" aria-labelledby="your-devices">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h2 id="your-devices" className="section-title">
              {t('devices.title')}
            </h2>
            <p className="mt-1 text-sm text-ink-muted">{t('devices.description')}</p>
          </div>
          {!connecting ? (
            <button type="button" className="btn-secondary btn-sm" onClick={() => setConnecting(true)}>
              {t('devices.connect')}
            </button>
          ) : null}
        </div>
        {devices.isLoading ? <Skeleton label={t('devices.title')} shape="rows" count={2} /> : null}
        {devices.isError ? (
          <LoadError compact error={devices.error} onRetry={() => void devices.refetch()} retrying={devices.isFetching} />
        ) : null}
        {devices.isSuccess && list.length === 0 ? <p className="text-sm text-ink-faint">{t('devices.none')}</p> : null}
        {list.length > 0 ? (
          <ul className="divide-y divide-line">
            {list.map((d) => (
              <li key={d.id} className="flex flex-wrap items-center justify-between gap-2 py-2">
                <div className="min-w-0">
                  <p className="truncate text-sm font-medium text-ink">
                    {d.name}
                    {d.push ? <span className="status-chip ms-2 bg-success/15 text-success">{t('devices.pushOn')}</span> : null}
                  </p>
                  <p className="text-xs text-ink-muted">
                    {d.last_used_at
                      ? t('devices.lastUsed', { date: formatDate(d.last_used_at, { dateStyle: 'medium', timeStyle: 'short' }) })
                      : t('devices.neverUsed')}
                  </p>
                </div>
                <button
                  type="button"
                  className="btn-secondary btn-sm"
                  disabled={disconnect.isPending}
                  onClick={() => {
                    if (window.confirm(t('devices.confirm', { name: d.name }))) disconnect.mutate(d.id)
                  }}
                >
                  {t('devices.disconnect')}
                </button>
              </li>
            ))}
          </ul>
        ) : null}
      </section>
      {connecting ? <ConnectPhone onClose={() => setConnecting(false)} /> : null}
    </>
  )
}
