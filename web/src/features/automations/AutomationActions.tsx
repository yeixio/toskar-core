import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { AutomationAction, Device } from '@/types/api'

const HA_CALL = 'homeassistant.call'

/** What can be done to a device of each kind, as Home Assistant services. */
const SERVICES: Partial<Record<Device['kind'], string[]>> = {
  light: ['turn_on', 'turn_off'],
  switch: ['turn_on', 'turn_off'],
  lock: ['lock', 'unlock'],
  cover: ['open_cover', 'close_cover'],
}

/** A device's services; scenes and scripts can be turned on. */
function servicesFor(d: Device): string[] {
  if (SERVICES[d.kind]) return SERVICES[d.kind]!
  if (d.device.startsWith('scene.') || d.device.startsWith('script.')) return ['turn_on']
  return []
}

/** The service an action calls, without its domain: light.turn_on gives turn_on. */
function serviceOf(a: AutomationAction): string {
  const s = a.args?.service ?? ''
  return s.includes('.') ? s.slice(s.indexOf('.') + 1) : s
}

export const MAX_ACTIONS = 20

/**
 * The calls an automation may make on its own (#586), picked from the
 * connected services' devices: "Turn on the porch light". A call that
 * matches runs without asking, even when a trigger started the run; any
 * other call that changes things is skipped.
 */
export function AutomationActions({ devices, value, onChange }: { devices: Device[]; value: AutomationAction[]; onChange: (next: AutomationAction[]) => void }) {
  const { t } = useTranslation('automations')
  const controllable = devices.filter((d) => d.connector === 'homeassistant' && servicesFor(d).length > 0)
  const [device, setDevice] = useState('')
  const [service, setService] = useState('')
  if (controllable.length === 0 && value.length === 0) return null

  const picked = controllable.find((d) => d.device === device)
  const services = picked ? servicesFor(picked) : []
  const chosenService = services.includes(service) ? service : (services[0] ?? '')
  const label = (a: AutomationAction) => {
    if (a.tool !== HA_CALL) {
      const args = Object.entries(a.args ?? {}).map(([k, v]) => `${k} ${v}`)
      return args.length ? `${a.tool} (${args.join(', ')})` : a.tool
    }
    const entity = a.args?.entity_id ?? ''
    const name = devices.find((d) => d.device === entity)?.name ?? entity
    const svc = serviceOf(a)
    return t('form.actionLabel', { action: t(`form.actionServices.${svc}`, { defaultValue: svc }), device: name })
  }
  const add = () => {
    if (!picked || !chosenService) return
    const domain = picked.device.split('.')[0]
    const next: AutomationAction = { tool: HA_CALL, args: { service: `${domain}.${chosenService}`, entity_id: picked.device } }
    if (value.some((a) => a.tool === HA_CALL && a.args?.entity_id === picked.device && serviceOf(a) === chosenService)) return
    onChange([...value, next])
    setDevice('')
    setService('')
  }

  return (
    <fieldset className="space-y-2 text-sm">
      <legend className="text-ink-muted">{t('form.actions')}</legend>
      {value.length > 0 ? (
        <ul className="space-y-1">
          {value.map((a, i) => (
            <li key={`${a.tool}-${i}`} className="flex items-center justify-between gap-3">
              <span>{label(a)}</span>
              <button type="button" className="btn-secondary px-2 py-1 text-xs" aria-label={t('form.actionRemove', { action: label(a) })} onClick={() => onChange(value.filter((_, j) => j !== i))}>
                {t('form.actionRemoveButton')}
              </button>
            </li>
          ))}
        </ul>
      ) : null}
      {controllable.length > 0 && value.length < MAX_ACTIONS ? (
        <div className="flex flex-wrap items-end gap-2">
          <label className="block">
            <span className="sr-only">{t('form.actionDevice')}</span>
            <select className="field" aria-label={t('form.actionDevice')} value={device} onChange={(e) => setDevice(e.target.value)}>
              <option value="">{t('form.actionDevicePick')}</option>
              {controllable.map((d) => (
                <option key={d.device} value={d.device}>
                  {d.name}
                </option>
              ))}
            </select>
          </label>
          <label className="block">
            <span className="sr-only">{t('form.actionService')}</span>
            <select className="field" aria-label={t('form.actionService')} value={chosenService} disabled={!picked} onChange={(e) => setService(e.target.value)}>
              {services.map((s) => (
                <option key={s} value={s}>
                  {t(`form.actionServices.${s}`, { defaultValue: s })}
                </option>
              ))}
            </select>
          </label>
          <button type="button" className="btn-secondary px-3 py-1.5 text-xs" disabled={!picked} onClick={add}>
            {t('form.actionAdd')}
          </button>
        </div>
      ) : null}
      <p className="text-xs text-ink-faint">{t('form.actionsHint')}</p>
    </fieldset>
  )
}
