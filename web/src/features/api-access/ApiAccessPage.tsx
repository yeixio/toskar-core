import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import { LoadingSpinner } from '@/components/ui/LoadingSpinner'
import { api, forgetApiKey, rememberApiKey, storedApiKey } from '@/lib/api'
import { formatLastUsed } from '@/features/models/modelPresentation'
import { useUIStore } from '@/stores/uiStore'
import type { APIKeyRecord } from '@/types/api'
import { KeyPermissions } from './KeyPermissions'
import { RealmKicker } from '@/components/ui/Realm'

type ProbeState = 'checking' | 'ok' | 'fail'

type ApiProbeResult = {
  healthOk: boolean
  openaiOk: boolean
  openaiStatus: number | null
  detail: string
}

async function probeLocalApi(lanEnabled: boolean): Promise<ApiProbeResult> {
  let healthOk: boolean
  try {
    const health = await api.getHealth()
    healthOk = health?.status === 'ok'
    if (!healthOk) {
      return {
        healthOk: false,
        openaiOk: false,
        openaiStatus: null,
        detail: 'Health check responded, but the service is not ready.',
      }
    }
  } catch {
    return {
      healthOk: false,
      openaiOk: false,
      openaiStatus: null,
      detail: 'Could not reach the API on this computer.',
    }
  }

  let openaiStatus: number | null
  try {
    const headers: Record<string, string> = { Accept: 'application/json' }
    const key = storedApiKey()
    if (key) headers.Authorization = `Bearer ${key}`
    const res = await fetch('/v1/models', {
      method: 'GET',
      headers,
    })
    openaiStatus = res.status
  } catch {
    return {
      healthOk,
      openaiOk: false,
      openaiStatus: null,
      detail: 'Service is up, but the OpenAI-compatible endpoint did not respond.',
    }
  }

  const openaiOk = lanEnabled
    ? openaiStatus === 200 || openaiStatus === 401
    : openaiStatus === 200

  let detail = 'Local check passed.'
  if (!openaiOk) {
    detail =
      openaiStatus != null
        ? `OpenAI endpoint returned HTTP ${openaiStatus}.`
        : 'OpenAI endpoint did not respond.'
  } else if (lanEnabled && openaiStatus === 401) {
    detail = 'Responding. Other devices need a valid API key.'
  }

  return { healthOk, openaiOk, openaiStatus, detail }
}

function lanUrlFromNodeAddress(address: string | undefined, port: number): string | null {
  if (!address) return null
  const host = address.split(':')[0]?.trim()
  if (!host || host === '127.0.0.1' || host === 'localhost' || host === '::1') {
    return null
  }
  return `http://${host}:${port}/v1`
}

function listensBeyondLoopback(host: string): boolean {
  const normalized = host.trim().toLowerCase().replace(/^\[|\]$/g, '')
  return normalized !== '' && normalized !== 'localhost' && normalized !== '127.0.0.1' && normalized !== '::1'
}

function maskPrefix(prefix: string): string {
  const tip = prefix.slice(-4) || prefix
  return `••••••••••${tip}`
}

export function ApiAccessPage() {
  const queryClient = useQueryClient()
  const advancedMode = useUIStore((s) => s.advancedMode)
  const [newKeyName, setNewKeyName] = useState('')
  const [revealedSecret, setRevealedSecret] = useState<string | null>(null)
  const [revealedKeyId, setRevealedKeyId] = useState<string | null>(null)
  const [copiedField, setCopiedField] = useState<string | null>(null)
  const [probeTick, setProbeTick] = useState(0)
  const [lanConfirmOpen, setLanConfirmOpen] = useState(false)
  const [browserHasKey, setBrowserHasKey] = useState(() => Boolean(storedApiKey()))
  const [dialogKeyName, setDialogKeyName] = useState('This computer')
  const [docsOpen, setDocsOpen] = useState(false)
  const [showCreate, setShowCreate] = useState(false)

  const settingsQuery = useQuery({
    queryKey: ['settings'],
    queryFn: () => api.getSettings(),
    retry: false,
  })

  const keysQuery = useQuery({
    queryKey: ['api-keys'],
    queryFn: () => api.listApiKeys(),
    retry: false,
  })

  const nodesQuery = useQuery({
    queryKey: ['nodes'],
    queryFn: () => api.getNodes(),
    retry: false,
    staleTime: 30_000,
  })

  const lanEnabled = settingsQuery.data?.lan_api_enabled ?? false
  const port = settingsQuery.data?.api_port ?? 7331
  const bindHost = settingsQuery.data?.api_host ?? '127.0.0.1'

  const localEndpoint = `http://localhost:${port}/v1`
  const friendlyLocal = `localhost:${port}`

  const lanEndpoint = useMemo(() => {
    const local = (nodesQuery.data ?? []).find((n) => n.is_local)
    return lanUrlFromNodeAddress(local?.address, port)
  }, [nodesQuery.data, port])

  const probeQuery = useQuery({
    queryKey: ['api-access-probe', lanEnabled, probeTick],
    queryFn: () => probeLocalApi(lanEnabled),
    retry: false,
    refetchInterval: 8_000,
  })

  const updateSettingsMutation = useMutation({
    mutationFn: (enabled: boolean) =>
      api.updateSettings({ lan_api_enabled: enabled }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['settings'] })
      setProbeTick((n) => n + 1)
      setLanConfirmOpen(false)
    },
  })

  const createKeyMutation = useMutation({
    mutationFn: (name: string) => api.createApiKey(name),
    onSuccess: (result) => {
      if (result?.secret) {
        setRevealedSecret(result.secret)
        setRevealedKeyId(result.key?.id ?? null)
        rememberApiKey(result.secret, result.key?.id)
        setBrowserHasKey(true)
      }
      setNewKeyName('')
      setShowCreate(false)
      queryClient.invalidateQueries({ queryKey: ['api-keys'] })
    },
  })

  const rotateKeyMutation = useMutation({
    mutationFn: (id: string) => api.rotateApiKey(id),
    onSuccess: (result) => {
      if (result?.secret) {
        setRevealedSecret(result.secret)
        setRevealedKeyId(result.key?.id ?? null)
        rememberApiKey(result.secret, result.key?.id)
        setBrowserHasKey(true)
      }
      queryClient.invalidateQueries({ queryKey: ['api-keys'] })
    },
  })

  const deleteKeyMutation = useMutation({
    mutationFn: (id: string) => api.deleteApiKey(id),
    onSuccess: (_data, id) => {
      if (revealedKeyId === id) {
        setRevealedSecret(null)
        setRevealedKeyId(null)
      }
      forgetApiKey(id)
      setBrowserHasKey(Boolean(storedApiKey()))
      queryClient.invalidateQueries({ queryKey: ['api-keys'] })
    },
  })

  const settings = settingsQuery.data
  const keys = keysQuery.data ?? []
  const activeKeys = keys.filter((k) => !k.revoked)
  const probe = probeQuery.data
  const probeState: ProbeState = probeQuery.isLoading
    ? 'checking'
    : probe && probe.healthOk && probe.openaiOk
      ? 'ok'
      : 'fail'

  const serviceLabel =
    probeState === 'ok' ? 'Running' : probeState === 'fail' ? 'Not responding' : 'Checking…'
  const accessLabel = lanEnabled ? 'Local network' : 'This computer only'
  const authRequired = lanEnabled || listensBeyondLoopback(bindHost)

  const copyText = async (field: string, value: string) => {
    try {
      await navigator.clipboard.writeText(value)
      setCopiedField(field)
      setTimeout(() => setCopiedField(null), 2000)
    } catch {
      // ignore
    }
  }

  const runTest = () => {
    setProbeTick((n) => n + 1)
    void queryClient.invalidateQueries({ queryKey: ['api-access-probe'] })
  }

  const requestLanEnable = (enabled: boolean) => {
    if (enabled) {
      setLanConfirmOpen(true)
      return
    }
    updateSettingsMutation.mutate(false)
  }

  return (
    <div className="mx-auto w-full max-w-2xl min-w-0 space-y-6">
      <header className="page-header">
        <RealmKicker />
        <h1 className="page-title">API Access</h1>
        <p className="page-subtitle">
          Where Yggdrasil&apos;s API is reachable, and how it is secured.
        </p>
      </header>

      {(settingsQuery.isLoading || keysQuery.isLoading) && (
        <LoadingSpinner label="Loading API settings…" />
      )}

      <section className="card space-y-4">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h2 className="section-title">Service</h2>
            <dl className="mt-3 space-y-2 text-sm">
              <div className="flex items-center gap-2">
                <dt className="text-ink-muted">API service</dt>
                <dd className="flex items-center gap-2 font-medium text-ink">
                  <span
                    className={[
                      'h-2 w-2 rounded-full',
                      probeState === 'ok'
                        ? 'bg-success'
                        : probeState === 'fail'
                          ? 'bg-danger'
                          : 'animate-pulse bg-warning',
                    ].join(' ')}
                    aria-hidden
                  />
                  {serviceLabel}
                </dd>
              </div>
              <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
                <dt className="text-ink-muted">Access</dt>
                <dd className="font-medium text-ink">{accessLabel}</dd>
              </div>
              <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
                <dt className="text-ink-muted">Authentication</dt>
                <dd className="font-medium text-ink">
                  {authRequired ? 'API key required' : 'Not required on this computer'}
                </dd>
              </div>
            </dl>
            {probeQuery.isError || probeState === 'fail' ? (
              <p className="mt-2 text-sm text-danger">
                {probeQuery.isError
                  ? 'Could not check the API — is the local service running?'
                  : (probe?.detail ?? 'API check failed.')}
              </p>
            ) : probe?.detail && probeState === 'ok' && lanEnabled ? (
              <p className="mt-2 text-xs text-ink-muted">{probe.detail}</p>
            ) : null}
          </div>
          <button
            type="button"
            className="btn-secondary shrink-0 px-3 py-1.5 text-xs"
            onClick={runTest}
            disabled={probeQuery.isFetching}
          >
            {probeQuery.isFetching ? 'Testing…' : 'Test API'}
          </button>
        </div>

        <div className="space-y-2">
          <p className="text-xs font-medium uppercase tracking-wide text-ink-faint">
            Endpoint
          </p>
          <CopyField
            value={localEndpoint}
            copied={copiedField === 'local-endpoint'}
            onCopy={() => void copyText('local-endpoint', localEndpoint)}
          />
          <p className="text-xs text-ink-muted">
            Local address:{' '}
            <span className="font-mono text-ink">{friendlyLocal}</span>
          </p>
          {lanEnabled && (
            <div className="space-y-1 pt-1">
              <p className="text-xs font-medium uppercase tracking-wide text-ink-faint">
                LAN endpoint
              </p>
              {lanEndpoint ? (
                <CopyField
                  value={lanEndpoint}
                  copied={copiedField === 'lan-endpoint'}
                  onCopy={() => void copyText('lan-endpoint', lanEndpoint)}
                />
              ) : (
                <p className="text-sm text-ink-muted">
                  Use this computer&apos;s LAN IP with port{' '}
                  <span className="font-mono text-ink">{port}</span>
                  {' '}
                  (for example <span className="font-mono text-ink">http://192.168.x.x:{port}/v1</span>
                  ).
                </p>
              )}
            </div>
          )}
        </div>

        <button
          type="button"
          className="text-sm font-medium text-primary underline-offset-2 hover:underline"
          onClick={() => setDocsOpen((o) => !o)}
          aria-expanded={docsOpen}
        >
          {docsOpen ? 'Hide API documentation' : 'API documentation'}
        </button>
        {docsOpen && (
          <div className="rounded-lg bg-raised/60 px-4 py-3 text-sm text-ink-muted">
            <p>
              Yggdrasil exposes an OpenAI-compatible API at{' '}
              <span className="font-mono text-ink">/v1</span>. Point clients at the endpoint
              above and send{' '}
              <span className="font-mono text-ink">Authorization: Bearer &lt;api-key&gt;</span>{' '}
              when Yggdrasil is reachable from other computers. `/api/v1` and `/v1` both
              require that header. A key does not encrypt plain HTTP.
            </p>
            <pre className="mt-3 overflow-x-auto rounded-md bg-canvas px-3 py-2 font-mono text-[11px] text-ink">
              {`curl ${localEndpoint}/models \\
  -H "Authorization: Bearer YOUR_KEY"`}
            </pre>
          </div>
        )}
      </section>

      <section className="card space-y-4">
        <div className="flex items-start justify-between gap-4">
          <div>
            <h2 className="section-title">Local network access</h2>
            <p className="mt-1 text-sm leading-relaxed text-ink-muted">
              {lanEnabled
                ? 'Devices on your local network can connect using an API key.'
                : 'Only apps on this computer can use Yggdrasil’s API.'}
            </p>
          </div>
          <Toggle
            label="Local network access"
            checked={lanEnabled}
            disabled={updateSettingsMutation.isPending}
            onChange={() => requestLanEnable(!lanEnabled)}
          />
        </div>

        {lanEnabled && activeKeys.length === 0 && (
          <div className="rounded-lg border border-warning/30 bg-warning/10 px-4 py-3 text-sm text-ink">
            Network access is on, but there are no API keys yet. Create a key before connecting
            from another device.
          </div>
        )}

        {lanEnabled && (
          <p className="text-xs text-ink-faint">
            After changing network access, fully quit and reopen Yggdrasil so the server rebinds.
            An API key is required for every connection once the listener is no longer loopback.
            A key does not encrypt traffic on plain HTTP. Test API only checks this computer —
            not reachability from other devices.
          </p>
        )}
      </section>

      <section className="card space-y-4">
        <div className="flex flex-wrap items-end justify-between gap-3">
          <div>
            <h2 className="section-title">API keys</h2>
            <p className="mt-1 text-sm text-ink-muted">
              Credentials for apps and devices that talk to Yggdrasil.
            </p>
          </div>
          {!showCreate && (
            <button
              type="button"
              className="btn-primary px-3 py-1.5 text-xs"
              onClick={() => setShowCreate(true)}
            >
              Create API key
            </button>
          )}
        </div>

        {showCreate && (
          <div className="space-y-3 rounded-lg border border-line bg-raised/40 px-4 py-3">
            <div>
              <h3 className="text-sm font-semibold text-ink">Create API key</h3>
              <p className="mt-1 text-sm text-ink-muted">
                Give this key a name so you know what uses it.
              </p>
            </div>
            <input
              type="text"
              value={newKeyName}
              onChange={(e) => setNewKeyName(e.target.value)}
              placeholder="VS Code"
              className="field w-full"
              autoFocus
            />
            <div className="flex gap-2">
              <button
                type="button"
                className="btn-primary px-3 py-1.5 text-xs"
                disabled={createKeyMutation.isPending || !newKeyName.trim()}
                onClick={() => createKeyMutation.mutate(newKeyName.trim())}
              >
                {createKeyMutation.isPending ? 'Creating…' : 'Create key'}
              </button>
              <button
                type="button"
                className="btn-secondary px-3 py-1.5 text-xs"
                onClick={() => {
                  setShowCreate(false)
                  setNewKeyName('')
                }}
              >
                Cancel
              </button>
            </div>
          </div>
        )}

        {revealedSecret && (
          <div className="rounded-lg border border-primary/30 bg-primary-soft px-4 py-3">
            <p className="text-xs font-medium uppercase tracking-wide text-ink-muted">
              Copy this secret now — it won&apos;t be shown again
            </p>
            <code className="mt-2 block break-all font-mono text-sm text-ink">
              {revealedSecret}
            </code>
            <button
              type="button"
              className="btn-secondary mt-3 px-3 py-1.5 text-xs"
              onClick={() => void copyText('secret', revealedSecret)}
            >
              {copiedField === 'secret' ? 'Copied!' : 'Copy secret'}
            </button>
          </div>
        )}

        {activeKeys.length === 0 && !keysQuery.isLoading && (
          <p className="text-sm text-ink-muted">No API keys yet.</p>
        )}

        {activeKeys.length > 0 && (
          <ul className="divide-y divide-line">
            {activeKeys.map((key: APIKeyRecord) => (
              <li key={key.id} className="flex flex-wrap items-start justify-between gap-3 py-3">
                <div className="min-w-0">
                  <p className="font-medium text-ink">{key.name}</p>
                  <p className="mt-0.5 text-xs text-ink-muted">
                    Created{' '}
                    {new Date(key.created_at).toLocaleDateString(undefined, {
                      month: 'short',
                      day: 'numeric',
                    })}
                    {' · '}
                    Last used {formatLastUsed(key.last_used_at)}
                  </p>
                  <p className="mt-1 font-mono text-xs text-ink-faint">
                    {maskPrefix(key.prefix)}
                  </p>
                </div>
                <div className="flex flex-wrap gap-2">
                  {revealedKeyId === key.id && revealedSecret ? (
                    <button
                      type="button"
                      className="btn-secondary px-3 py-1.5 text-xs"
                      onClick={() => void copyText(`key-${key.id}`, revealedSecret)}
                    >
                      {copiedField === `key-${key.id}` ? 'Copied!' : 'Copy'}
                    </button>
                  ) : null}
                  <button
                    type="button"
                    className="btn-secondary px-3 py-1.5 text-xs"
                    disabled={rotateKeyMutation.isPending}
                    onClick={() => {
                      if (
                        window.confirm(
                          `Rotate “${key.name}”? The old secret stops working immediately.`,
                        )
                      ) {
                        rotateKeyMutation.mutate(key.id)
                      }
                    }}
                  >
                    Rotate
                  </button>
                  <button
                    type="button"
                    className="btn-secondary px-3 py-1.5 text-xs"
                    disabled={deleteKeyMutation.isPending}
                    onClick={() => {
                      if (
                        window.confirm(
                          `Revoke “${key.name}”? Apps using this key will lose access.`,
                        )
                      ) {
                        deleteKeyMutation.mutate(key.id)
                      }
                    }}
                  >
                    Revoke
                  </button>
                </div>
                <KeyPermissions apiKey={key} />
              </li>
            ))}
          </ul>
        )}
      </section>

      {advancedMode && settings && (
        <section className="card space-y-3">
          <h2 className="section-title">Advanced</h2>
          <dl className="space-y-2 text-sm">
            <div className="flex justify-between gap-3">
              <dt className="text-ink-muted">Bind address</dt>
              <dd className="font-mono text-ink">
                {bindHost}:{port}
              </dd>
            </div>
            <div className="flex justify-between gap-3">
              <dt className="text-ink-muted">Port</dt>
              <dd className="font-mono text-ink">{port}</dd>
            </div>
            <div className="flex justify-between gap-3">
              <dt className="text-ink-muted">Configured host</dt>
              <dd className="font-mono text-ink">{settings.api_host}</dd>
            </div>
          </dl>
        </section>
      )}

      {lanConfirmOpen && (
        <div
          className="fixed inset-0 z-40 flex items-center justify-center bg-black/40 p-4"
          role="dialog"
          aria-modal="true"
          aria-labelledby="lan-confirm-title"
        >
          <div className="w-full max-w-md rounded-panel border border-line bg-surface p-5 shadow-panel">
            <h2 id="lan-confirm-title" className="font-display text-lg font-semibold text-ink">
              Allow access from other computers?
            </h2>
            <p className="mt-2 text-sm leading-relaxed text-ink-muted">
              Devices on your local network will be able to connect to Yggdrasil. An API key is
              required for all remote connections.
            </p>
            <p className="mt-2 text-sm leading-relaxed text-ink-muted">
              An API key over plain HTTP does not encrypt traffic. Quit and reopen Yggdrasil after
              enabling this so the server listens on the network.
            </p>
            {!browserHasKey && (
              <div className="mt-4 space-y-2">
                <p className="text-sm text-ink">
                  Create an API key so this browser can keep connecting after the server rebinds.
                  The full key is shown only once.
                </p>
                <label className="block text-sm font-medium text-ink" htmlFor="lan-key-name">
                  Key name
                </label>
                <input
                  id="lan-key-name"
                  type="text"
                  value={dialogKeyName}
                  onChange={(e) => setDialogKeyName(e.target.value)}
                  className="field w-full"
                />
                <button
                  type="button"
                  className="btn-secondary px-3 py-1.5 text-xs"
                  disabled={createKeyMutation.isPending || !dialogKeyName.trim()}
                  onClick={() => createKeyMutation.mutate(dialogKeyName.trim())}
                >
                  {createKeyMutation.isPending ? 'Creating…' : 'Create key'}
                </button>
              </div>
            )}
            {revealedSecret && (
              <p className="mt-3 break-all font-mono text-xs text-ink">{revealedSecret}</p>
            )}
            {updateSettingsMutation.isError && (
              <p className="mt-3 text-sm text-warning">
                {updateSettingsMutation.error instanceof Error
                  ? updateSettingsMutation.error.message
                  : 'Could not enable network access.'}
              </p>
            )}
            <div className="mt-5 flex justify-end gap-2">
              <button
                type="button"
                className="btn-secondary px-3 py-1.5 text-xs"
                onClick={() => setLanConfirmOpen(false)}
                disabled={updateSettingsMutation.isPending}
              >
                Cancel
              </button>
              <button
                type="button"
                className="btn-primary px-3 py-1.5 text-xs"
                disabled={
                  updateSettingsMutation.isPending || activeKeys.length === 0 || !browserHasKey
                }
                onClick={() => updateSettingsMutation.mutate(true)}
              >
                {updateSettingsMutation.isPending ? 'Enabling…' : 'Allow access'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

function CopyField({
  value,
  copied,
  onCopy,
}: {
  value: string
  copied: boolean
  onCopy: () => void
}) {
  return (
    <div className="flex min-w-0 items-stretch gap-2">
      <code className="field min-w-0 flex-1 truncate py-2 font-mono text-xs text-ink">
        {value}
      </code>
      <button type="button" className="btn-secondary shrink-0 px-3 py-1.5 text-xs" onClick={onCopy}>
        {copied ? 'Copied' : 'Copy URL'}
      </button>
    </div>
  )
}

function Toggle({
  label,
  checked,
  disabled,
  onChange,
}: {
  label: string
  checked: boolean
  disabled?: boolean
  onChange: () => void
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      disabled={disabled}
      onClick={onChange}
      className={[
        'relative inline-flex h-7 w-12 shrink-0 rounded-full transition disabled:opacity-50',
        checked ? 'bg-primary' : 'bg-raised',
      ].join(' ')}
    >
      <span
        className={[
          'absolute top-0.5 h-6 w-6 rounded-full bg-[#EEF2F6] shadow transition',
          checked ? 'left-[22px]' : 'left-0.5',
        ].join(' ')}
      />
    </button>
  )
}
