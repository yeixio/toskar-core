import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type ReachCheck, type ReachWay } from '@/lib/api'
import { formatRelativeTime } from '@/i18n/format'
import { Toggle } from './Toggle'

/**
 * Access from anywhere (#456, docs/remote-access.md): a listener for the
 * devices paired with this computer when they're away from home. It serves
 * only what a phone uses, over HTTPS, to their keys. The router opens a
 * port for it, or the person forwards one; otherwise a relay carries the
 * connection: Toskar's, or the organization's own.
 */
export function RemoteAccessCard() {
  const { t } = useTranslation('apiAccess')
  const queryClient = useQueryClient()
  const settings = useQuery({ queryKey: ['settings'], queryFn: () => api.getSettings() })
  const status = useQuery({ queryKey: ['remote-access'], queryFn: () => api.getRemoteAccess(), refetchInterval: 15_000 })
  const enabled = settings.data?.remote_access_enabled ?? false
  const [port, setPort] = useState('')
  const [address, setAddress] = useState('')
  const [error, setError] = useState('')
  useEffect(() => {
    if (settings.data) {
      setPort(String(settings.data.remote_access_port ?? 7333))
      setAddress(settings.data.remote_access_address ?? '')
    }
  }, [settings.data])
  const save = useMutation({
    mutationFn: (patch: Parameters<typeof api.updateSettings>[0]) => api.updateSettings(patch),
    onSuccess: () => {
      setError('')
      void queryClient.invalidateQueries({ queryKey: ['settings'] })
      void queryClient.invalidateQueries({ queryKey: ['remote-access'] })
    },
    onError: (err) => setError(err instanceof Error ? err.message : String(err)),
  })
  const st = status.data
  const current = String(settings.data?.remote_access_port ?? 7333)
  const changed = port !== current || address.trim() !== (settings.data?.remote_access_address ?? '')
  return (
    <section className="card space-y-4" aria-labelledby="remote-access">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h2 id="remote-access" className="section-title">
            {t('remote.title')}
          </h2>
          <p className="mt-1 text-sm leading-relaxed text-ink-muted">{t('remote.body')}</p>
        </div>
        <Toggle
          label={t('remote.title')}
          checked={enabled}
          disabled={save.isPending || !settings.data}
          onChange={() => save.mutate({ remote_access_enabled: !enabled })}
        />
      </div>
      {enabled && st ? <OutsideCheck status={st} /> : null}
      <label className="flex items-center gap-2 text-sm">
        <input
          type="checkbox"
          checked={settings.data?.remote_access_port_mapping ?? true}
          disabled={save.isPending || !settings.data}
          onChange={(e) => save.mutate({ remote_access_port_mapping: e.target.checked })}
        />
        <span>{t('remote.portMapping')}</span>
      </label>
      <form
        className="grid gap-3 sm:grid-cols-[8rem_1fr_auto] sm:items-end"
        onSubmit={(e) => {
          e.preventDefault()
          save.mutate({ remote_access_port: Number(port) || 0, remote_access_address: address.trim() })
        }}
      >
        <label className="block space-y-1 text-sm">
          <span className="text-ink-muted">{t('remote.port')}</span>
          <input className="field w-full" inputMode="numeric" value={port} onChange={(e) => setPort(e.target.value.replace(/\D/g, ''))} />
        </label>
        <label className="block space-y-1 text-sm">
          <span className="text-ink-muted">{t('remote.address')}</span>
          <input
            className="field w-full"
            placeholder={t('remote.addressExample')}
            value={address}
            onChange={(e) => setAddress(e.target.value)}
          />
        </label>
        <button type="submit" className="btn-secondary" disabled={!changed || save.isPending}>
          {t('remote.save')}
        </button>
      </form>
      {error ? <p className="text-sm text-danger">{error}</p> : null}
      <p className="text-xs text-ink-faint">{t('remote.notes', { port: current })}</p>
      <RelaySettings
        relay={settings.data?.remote_access_relay ?? ''}
        enrolled={settings.data?.remote_access_relay_enrolled ?? false}
        status={enabled ? st?.relay : undefined}
        saving={save.isPending}
        onSave={(patch) => save.mutate(patch)}
      />
    </section>
  )
}

type RelayStatus = NonNullable<Awaited<ReturnType<typeof api.getRemoteAccess>>>['relay']

/**
 * An organization's own relay (toskar-relay's self-hosting guide): its name
 * and the enrollment secret this computer trades for its own token. The
 * secret is never shown again; the field only says one is saved.
 */
function RelaySettings({
  relay,
  enrolled,
  status,
  saving,
  onSave,
}: {
  relay: string
  enrolled: boolean
  status: RelayStatus
  saving: boolean
  onSave: (patch: { remote_access_relay?: string; remote_access_relay_secret?: string }) => void
}) {
  const { t } = useTranslation('apiAccess')
  const [name, setName] = useState(relay)
  const [secret, setSecret] = useState('')
  useEffect(() => setName(relay), [relay])
  const own = relay !== ''
  // The secret is only ever for an organization's relay, never Toskar's.
  const changed = name.trim() !== '' && (name.trim() !== relay || secret.trim() !== '')
  return (
    <details className="rounded-lg border border-line p-3" open={own || undefined}>
      <summary className="cursor-pointer text-sm font-medium">{t('remote.relay.title')}</summary>
      <div className="mt-3 space-y-3">
        <p className="text-sm text-ink-muted">{t('remote.relay.body')}</p>
        {own && status ? <RelayLine status={status} /> : null}
        <form
          className="grid gap-3 sm:grid-cols-[1fr_1fr_auto] sm:items-end"
          onSubmit={(e) => {
            e.preventDefault()
            const patch: { remote_access_relay?: string; remote_access_relay_secret?: string } = {}
            if (name.trim() !== relay) patch.remote_access_relay = name.trim()
            if (secret.trim() !== '') patch.remote_access_relay_secret = secret.trim()
            onSave(patch)
            setSecret('')
          }}
        >
          <label className="block space-y-1 text-sm">
            <span className="text-ink-muted">{t('remote.relay.name')}</span>
            <input
              className="field w-full"
              placeholder={t('remote.relay.nameExample')}
              autoComplete="off"
              spellCheck={false}
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </label>
          <label className="block space-y-1 text-sm">
            <span className="text-ink-muted">{t('remote.relay.secret')}</span>
            <input
              className="field w-full"
              type="password"
              autoComplete="off"
              placeholder={enrolled ? t('remote.relay.secretSaved') : t('remote.relay.secretPlaceholder')}
              value={secret}
              onChange={(e) => setSecret(e.target.value)}
            />
          </label>
          <button type="submit" className="btn-secondary" disabled={!changed || saving}>
            {t('remote.relay.connect')}
          </button>
        </form>
        {own ? (
          <button
            type="button"
            className="btn-ghost text-sm"
            disabled={saving}
            onClick={() => onSave({ remote_access_relay: '', remote_access_relay_secret: '' })}
          >
            {t('remote.relay.remove')}
          </button>
        ) : null}
      </div>
    </details>
  )
}

/** How the connection to the organization's relay is doing. */
function RelayLine({ status }: { status: NonNullable<RelayStatus> }) {
  const { t } = useTranslation('apiAccess')
  switch (status.state) {
    case 'connected':
      return (
        <p className="text-sm text-success" role="status">
          {t('remote.relay.state.connected', { relay: status.name })}
        </p>
      )
    case 'no_token':
      return (
        <p className="text-sm text-warning" role="status">
          {t('remote.relay.state.noToken')}
        </p>
      )
    case 'error':
      return (
        <p className="text-sm text-danger" role="status">
          {t('remote.relay.state.error', { relay: status.name, code: status.error ?? '' })}
        </p>
      )
    default:
      return (
        <p className="text-sm text-ink-muted" role="status">
          {t('remote.relay.state.connecting', { relay: status.name })}
        </p>
      )
  }
}

type Status = NonNullable<Awaited<ReturnType<typeof api.getRemoteAccess>>>

/**
 * Whether access from anywhere works, as the relay found it from the
 * internet (#546), like Plex's "fully accessible outside your network": a
 * line in green or red, when it was checked, Check now, and a row for each
 * way in with what to do when it fails. Before the first check, the line
 * says how Toskar set it up, as before.
 */
function OutsideCheck({ status }: { status: Status }) {
  const { t } = useTranslation('apiAccess')
  const queryClient = useQueryClient()
  const check = useMutation({
    mutationFn: () => api.checkRemoteAccess(),
    onSuccess: (data) => queryClient.setQueryData(['remote-access'], data),
  })
  if (!status.listening) return <ReachLine status={status} />
  const result = status.check
  const ways = result?.at ? [...(result.relay ? [result.relay] : []), ...(result.direct ?? [])] : []
  const reachable = ways.some((w) => w.ok)
  const summary = !result?.at ? 'unchecked' : reachable ? 'reachable' : 'unreachable'
  const tone = { reachable: 'text-success', unreachable: 'text-danger', unchecked: 'text-ink-muted' }[summary]
  const dot = { reachable: 'bg-success', unreachable: 'bg-danger', unchecked: 'bg-ink-faint' }[summary]
  return (
    <div className="space-y-3 rounded-lg border border-line p-3" role="status" aria-live="polite">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className={`flex items-center gap-2 text-sm font-medium ${tone}`}>
          <span className={`inline-block h-2.5 w-2.5 rounded-full ${dot}`} aria-hidden />
          {t(`remote.check.${summary}`)}
        </p>
        <div className="flex items-center gap-3 text-xs text-ink-faint">
          {result?.at ? <span>{t('remote.check.checked', { when: formatRelativeTime(result.at) })}</span> : null}
          <button type="button" className="btn-secondary" disabled={check.isPending} onClick={() => check.mutate()}>
            {check.isPending ? t('remote.check.checking') : t('remote.check.now')}
          </button>
        </div>
      </div>
      {result?.at ? (
        <ul className="space-y-2">
          {result.relay ? <WayRow way={result.relay} label={t('remote.check.viaRelay', { relay: (status.relay?.name ?? '').replace(/:\d+$/, '') })} /> : null}
          {(result.direct ?? []).map((way) => (
            <WayRow key={way.address} way={way} label={way.address ?? ''} />
          ))}
        </ul>
      ) : (
        <ReachLine status={status} />
      )}
      {status.relay && status.relay.state !== 'connected' ? <ToskarRelayLine relay={status.relay} /> : null}
      <CheckProblem result={result} failed={check.error} />
      {result?.at && !reachable ? <p className="text-xs text-ink-muted">{t('remote.check.nextSteps')}</p> : null}
    </div>
  )
}

/** One way in: what it is, and whether the internet reached it. */
function WayRow({ way, label }: { way: ReachWay; label: string }) {
  const { t } = useTranslation('apiAccess')
  return (
    <li className="flex gap-2 text-sm">
      <span className={way.ok ? 'text-success' : 'text-danger'} aria-hidden>
        {way.ok ? '✓' : '✗'}
      </span>
      <span>
        <span className="font-medium">{label}</span>
        <span className="text-ink-muted">
          {' — '}
          {way.ok ? t('remote.check.ok', { ms: way.ms ?? 0 }) : t(`remote.check.reason.${way.reason ?? 'unreachable'}`)}
        </span>
      </span>
    </li>
  )
}

/** The relay connection when it isn't up, for Toskar's relay as for an organization's. */
function ToskarRelayLine({ relay }: { relay: NonNullable<Status['relay']> }) {
  const { t } = useTranslation('apiAccess')
  const key = relay.state === 'no_token' ? 'noToken' : relay.state === 'error' ? 'error' : 'connecting'
  return (
    <p className={`text-sm ${relay.state === 'connecting' ? 'text-ink-muted' : 'text-warning'}`}>
      {t(`remote.check.relayState.${key}`, { relay: relay.name, code: relay.error ?? '' })}
    </p>
  )
}

/** Why the last check didn't reach the relay, when it didn't. */
function CheckProblem({ result, failed }: { result?: ReachCheck; failed: Error | null }) {
  const { t } = useTranslation('apiAccess')
  const code = failed ? 'failed' : result?.error
  if (!code) return null
  const known = ['CHECK_UNSUPPORTED', 'CHECK_TOO_SOON', 'NO_TOKEN', 'RELAY_UNREACHABLE']
  const text = failed
    ? failed.message
    : known.includes(code)
      ? t(`remote.check.error.${code}`)
      : t('remote.check.error.other', { code })
  return <p className="text-xs text-warning">{text}</p>
}

/** How the internet reaches this computer, in a line. */
function ReachLine({ status }: { status: Status }) {
  const { t } = useTranslation('apiAccess')
  if (!status.listening) {
    return (
      <p className="text-sm text-danger" role="status">
        {t('remote.notListening', { port: status.port, error: status.error ?? '' })}
      </p>
    )
  }
  const by = status.mapped_by ? t(`remote.method.${status.mapped_by}`) : ''
  let line: string
  let ok = true
  switch (status.reachable) {
    case 'direct':
      line = t('remote.reach.direct', { address: status.mapped, method: by })
      break
    case 'ipv6':
      line = t('remote.reach.ipv6', { address: status.ipv6?.[0] ?? '', port: status.port })
      break
    case 'manual':
      line = t('remote.reach.manual', { address: status.address })
      break
    case 'relay':
      line = t('remote.reach.relay', { relay: status.relay?.name ?? '' })
      break
    default:
      ok = false
      line = status.reason === 'carrier_nat' ? t('remote.reach.carrierNat') : t('remote.reach.noPort', { port: status.port })
  }
  return (
    <p className={`text-sm ${ok ? 'text-success' : 'text-warning'}`} role="status">
      {line}
    </p>
  )
}
