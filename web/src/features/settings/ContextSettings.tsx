import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { canLocate, deviceLocationOn, locate, setDeviceLocationOn, type DevicePosition } from '@/lib/deviceLocation'
import type { PersonalStyle } from '@/types/api'
import { languages } from '@/i18n/languages'

/**
 * Use this device's location (#1310): a switch kept on this device. The
 * browser asks the person before saying where it is, once or each time.
 */
export function DeviceLocationSwitch() {
  const { t } = useTranslation('settings')
  const [on, setOn] = useState(deviceLocationOn)
  const [position, setPosition] = useState<DevicePosition | null>(null)
  const [problem, setProblem] = useState<'denied' | 'failed' | null>(null)
  const [asking, setAsking] = useState(false)
  if (!canLocate()) return <p className="text-xs text-ink-faint">{t('context.deviceUnavailable')}</p>

  const turn = (next: boolean) => {
    setProblem(null)
    if (!next) {
      setDeviceLocationOn(false)
      setOn(false)
      setPosition(null)
      return
    }
    setAsking(true)
    locate()
      .then((p) => {
        setDeviceLocationOn(true)
        setOn(true)
        setPosition(p)
      })
      .catch((err: { code?: number }) => {
        setDeviceLocationOn(false)
        setOn(false)
        setProblem(err?.code === 1 ? 'denied' : 'failed')
      })
      .finally(() => setAsking(false))
  }

  return (
    <div className="text-sm">
      <label className="flex items-start gap-2 text-ink">
        <input type="checkbox" className="mt-0.5" checked={on} disabled={asking} onChange={(e) => turn(e.target.checked)} />
        <span>
          <span className="font-medium">{t('context.device')}</span>
          <span className="block text-xs text-ink-muted">{t('context.deviceHint')}</span>
        </span>
      </label>
      <p className="mt-1 ps-6 text-xs text-ink-faint" role="status">
        {asking
          ? t('context.deviceAsking')
          : problem === 'denied'
            ? t('context.deviceDenied')
            : problem === 'failed'
              ? t('context.deviceFailed')
              : on && position?.accuracy_m
                ? t('context.deviceUsing', { meters: position.accuracy_m })
                : null}
      </p>
    </div>
  )
}

/**
 * What Toskar knows about where and when the person is (#1310), each with
 * where it comes from, so a wrong date or town can be traced and changed.
 */
export function ContextSummary({ style }: { style?: PersonalStyle }) {
  const { t, i18n } = useTranslation('settings')
  let zone: string | undefined
  try {
    zone = Intl.DateTimeFormat().resolvedOptions().timeZone || undefined
  } catch {
    zone = undefined
  }
  const now = new Intl.DateTimeFormat(i18n.language, { dateStyle: 'full', timeStyle: 'short', timeZone: zone }).format(new Date())
  const rows: [string, string][] = [
    [t('context.timeZone'), zone ? t('context.fromDevice', { value: zone }) : t('context.computerZone')],
    [t('context.now'), now],
    [t('context.language'), languages.find((l) => l.code === i18n.language)?.name ?? i18n.language],
    [t('context.units'), style?.units ? t(`personalization.units.${style.units}`) : t('context.unitsFromLanguage')],
    [t('context.place'), style?.location ? style.location : t('context.notSet')],
    [t('context.deviceRow'), deviceLocationOn() ? t('context.deviceOn') : t('context.deviceOff')],
  ]
  return (
    <div className="space-y-2">
      <h3 className="text-sm font-medium text-ink">{t('context.title')}</h3>
      <dl className="grid gap-x-4 gap-y-1 text-sm sm:grid-cols-[auto_1fr]">
        {rows.map(([label, value]) => (
          <div key={label} className="contents">
            <dt className="text-ink-muted">{label}</dt>
            <dd className="text-ink">{value}</dd>
          </div>
        ))}
      </dl>
      <p className="text-xs text-ink-faint">{t('context.kept')}</p>
    </div>
  )
}
