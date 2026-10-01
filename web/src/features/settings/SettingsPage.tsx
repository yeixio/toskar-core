import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { LoadingSpinner } from '@/components/ui/LoadingSpinner'
import { api } from '@/lib/api'
import { displayVersion } from '@/lib/appVersion'
import {
  isDesktopShell,
  notifyDesktopBackgroundMode,
  notifyDesktopLaunchAtLogin,
  openPathInOS,
  quitDesktopForRestart,
} from '@/lib/desktopBridge'
import { useUIStore } from '@/stores/uiStore'
import type { SettingsPatch } from '@/types/api'
import { RealmKicker } from '@/components/ui/Realm'
import { ConnectedServices } from './ConnectedServices'
import { Personalization } from './Personalization'
import { YggdrasilMark } from '@/components/ui/YggdrasilMark'

function Toggle({
  checked,
  disabled,
  label,
  onChange,
}: {
  checked: boolean
  disabled?: boolean
  label: string
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

function ChoiceGroup<T extends string>({
  value,
  options,
  onChange,
}: {
  value: T
  options: { id: T; label: string }[]
  onChange: (id: T) => void
}) {
  return (
    <div className="flex flex-wrap gap-2">
      {options.map((option) => {
        const active = value === option.id
        return (
          <button
            key={option.id}
            type="button"
            onClick={() => onChange(option.id)}
            className={[
              'rounded-lg px-4 py-2 text-sm font-medium transition',
              active
                ? 'bg-primary-soft text-primary-active'
                : 'bg-raised/70 text-ink-muted hover:text-ink',
            ].join(' ')}
          >
            {option.label}
          </button>
        )
      })}
    </div>
  )
}

function PathRow({
  label,
  path,
  openLabel,
}: {
  label: string
  path?: string
  openLabel: string
}) {
  const [status, setStatus] = useState<'idle' | 'opened' | 'failed'>('idle')
  return (
    <div className="min-w-0 space-y-1.5">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="text-sm font-medium text-ink">{label}</p>
        {path ? (
          <button
            type="button"
            className="text-xs text-primary hover:underline"
            onClick={async () => {
              const ok = await openPathInOS(path)
              setStatus(ok ? 'opened' : 'failed')
              setTimeout(() => setStatus('idle'), 2000)
            }}
          >
            {status === 'opened'
              ? 'Opened'
              : status === 'failed'
                ? 'Copy path from below'
                : openLabel}
          </button>
        ) : null}
      </div>
      <p className="break-anywhere font-mono text-xs text-ink-muted" title={path}>
        {path ?? 'Will appear when Yggdrasil is running'}
      </p>
    </div>
  )
}

function openFolderLabel(): string {
  const ua = typeof navigator !== 'undefined' ? navigator.userAgent : ''
  if (/Mac/i.test(ua)) return 'Show in Finder'
  if (/Win/i.test(ua)) return 'Open folder'
  return 'Open folder'
}

export function SettingsPage() {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const advancedMode = useUIStore((s) => s.advancedMode)
  const setAdvancedMode = useUIStore((s) => s.setAdvancedMode)
  const theme = useUIStore((s) => s.theme)
  const setTheme = useUIStore((s) => s.setTheme)
  const resetToDefaults = useUIStore((s) => s.resetToDefaults)
  const [confirmReset, setConfirmReset] = useState(false)
  const [deleteModels, setDeleteModels] = useState(false)
  const [resetError, setResetError] = useState<string | null>(null)
  const folderLabel = openFolderLabel()

  const settingsQuery = useQuery({
    queryKey: ['settings'],
    queryFn: () => api.getSettings(),
    retry: false,
  })

  const versionQuery = useQuery({
    queryKey: ['version'],
    queryFn: () => api.getVersion(),
    retry: false,
  })

  const healthQuery = useQuery({
    queryKey: ['health'],
    queryFn: () => api.getHealth(),
    retry: false,
  })

  const profilesQuery = useQuery({
    queryKey: ['profiles'],
    queryFn: () => api.getProfiles(),
    retry: false,
  })
  const scheduleQuery = useQuery({
    queryKey: ['automations'],
    queryFn: () => api.listAutomations(),
    retry: false,
  })
  const hasSchedule = (scheduleQuery.data?.length ?? 0) > 0

  useEffect(() => {
    if (settingsQuery.data?.advanced_mode != null) {
      setAdvancedMode(settingsQuery.data.advanced_mode)
    }
  }, [settingsQuery.data?.advanced_mode, setAdvancedMode])

  const patchMutation = useMutation({
    mutationFn: (patch: SettingsPatch) => api.updateSettings(patch),
    onSuccess: (settings) => {
      if (settings?.advanced_mode != null) {
        setAdvancedMode(settings.advanced_mode)
      }
      queryClient.invalidateQueries({ queryKey: ['settings'] })
      queryClient.invalidateQueries({ queryKey: ['nodes'] })
    },
  })

  const backgroundMutation = useMutation({
    mutationFn: async (enabled: boolean) => {
      const settings = await api.updateSettings({
        keep_running_in_background: enabled,
      })
      await notifyDesktopBackgroundMode(enabled)
      return settings
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['settings'] })
    },
  })

  const launchAtLoginMutation = useMutation({
    mutationFn: async (enabled: boolean) => {
      const settings = await api.updateSettings({ launch_at_login: enabled })
      await notifyDesktopLaunchAtLogin(enabled)
      return settings
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['settings'] })
    },
  })

  const resetMutation = useMutation({
    mutationFn: async () => {
      const settings = await api.resetApp({ delete_models: deleteModels })
      await notifyDesktopLaunchAtLogin(false)
      return settings
    },
    onSuccess: () => {
      resetToDefaults()
      queryClient.clear()
      navigate('/onboarding', { replace: true })
    },
    onError: (error) => {
      setConfirmReset(false)
      setResetError(
        error instanceof Error
          ? error.message
          : 'Could not reset the application. Try again.',
      )
    },
  })

  const clearHistoryMutation = useMutation({
    mutationFn: async () => {
      const conversations = (await api.getConversations()) ?? []
      for (const c of conversations) {
        await api.deleteConversation(c.id)
      }
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['conversations'] })
      queryClient.invalidateQueries({ queryKey: ['performance'] })
    },
  })

  const settings = settingsQuery.data
  const profiles = profilesQuery.data ?? []

  const patch = (body: SettingsPatch) => patchMutation.mutate(body)
  const busy = patchMutation.isPending || settingsQuery.isLoading

  return (
    <div className="mx-auto w-full max-w-2xl min-w-0 space-y-6">
      <header className="page-header">
        <RealmKicker />
        <h1 className="page-title">Settings</h1>
        <p className="page-subtitle">How Yggdrasil should behave on this computer.</p>
      </header>

      {(settingsQuery.isLoading || versionQuery.isLoading) && (
        <LoadingSpinner label="Loading settings…" />
      )}

      <div className="settings-group">
        <p className="settings-group-label">General</p>
        <section className="card space-y-4">
          <div>
            <h2 className="section-title">Appearance</h2>
            <p className="mt-1 text-sm text-ink-muted">
              Choose light, dark, or follow the system.
            </p>
          </div>
          <ChoiceGroup
            value={theme}
            onChange={setTheme}
            options={[
              { id: 'dark', label: 'Dark' },
              { id: 'light', label: 'Light' },
              { id: 'system', label: 'System' },
            ]}
          />
        </section>

        <Personalization />

        <section className="card space-y-4">
          <div className="flex items-start justify-between gap-4">
            <div>
              <h2 className="section-title">Keep running in background</h2>
              <p className="mt-1 text-sm text-ink-muted">
                Closing the window keeps Chat, the API, and your schedules running. This stays
                on while a schedule exists.
              </p>
            </div>
            <Toggle
              label="Keep running in background"
              checked={(settings?.keep_running_in_background ?? false) || hasSchedule}
              disabled={backgroundMutation.isPending || busy || hasSchedule}
              onChange={() =>
                backgroundMutation.mutate(!(settings?.keep_running_in_background ?? false))
              }
            />
          </div>
        </section>

        {isDesktopShell() && (
          <section className="card space-y-4">
            <div className="flex items-start justify-between gap-4">
              <div>
                <h2 className="section-title">Launch at login</h2>
                <p className="mt-1 text-sm text-ink-muted">
                  Start Yggdrasil automatically when you sign in to this computer.
                </p>
              </div>
              <Toggle
                label="Launch at login"
                checked={settings?.launch_at_login ?? false}
                disabled={launchAtLoginMutation.isPending || busy}
                onChange={() =>
                  launchAtLoginMutation.mutate(!(settings?.launch_at_login ?? false))
                }
              />
            </div>
          </section>
        )}
      </div>

      <div className="settings-group">
        <p className="settings-group-label">AI behavior</p>
        <section className="card space-y-4">
          <div>
            <h2 className="section-title">Default profile</h2>
            <p className="mt-1 text-sm text-ink-muted">
              Used when you start a new chat.
            </p>
          </div>
          <select
            className="field w-full"
            value={settings?.default_profile_id ?? ''}
            disabled={busy}
            onChange={(e) => patch({ default_profile_id: e.target.value })}
          >
            <option value="">Automatic</option>
            {profiles.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </select>
        </section>

        <section className="card space-y-4">
          <div>
            <h2 className="section-title">Run chats on</h2>
            <p className="mt-1 text-sm text-ink-muted">
              Where work should run by default across your team.
            </p>
          </div>
          <ChoiceGroup
            value={(settings?.default_execution ?? 'automatic') as 'automatic' | 'local' | 'ask'}
            onChange={(id) => patch({ default_execution: id })}
            options={[
              { id: 'automatic', label: 'Automatic' },
              { id: 'local', label: 'This computer' },
              { id: 'ask', label: 'Ask each time' },
            ]}
          />
        </section>

        <section className="card space-y-4">
          <div>
            <h2 className="section-title">Model lifecycle</h2>
            <p className="mt-1 text-sm text-ink-muted">
              Automatic keeps models warm after chat. Manual leaves Start and Stop to you.
            </p>
          </div>
          <ChoiceGroup
            value={(settings?.model_lifecycle ?? 'automatic') as 'automatic' | 'manual'}
            onChange={(id) => patch({ model_lifecycle: id })}
            options={[
              { id: 'automatic', label: 'Automatic' },
              { id: 'manual', label: 'Manual' },
            ]}
          />
          {(settings?.model_lifecycle ?? 'automatic') === 'automatic' && (
            <label className="block text-sm text-ink-muted">
              Unload idle models after
              <p className="mt-0.5 text-xs text-ink-faint">
                Frees memory when a model hasn’t been used for a while. Use 0 to keep models
                loaded.
              </p>
              <div className="mt-2 flex items-center gap-2">
                <input
                  type="number"
                  min={0}
                  max={240}
                  className="field max-w-[8rem]"
                  defaultValue={settings?.idle_unload_minutes ?? 15}
                  key={settings?.idle_unload_minutes ?? 15}
                  onBlur={(e) => {
                    const n = Number(e.target.value)
                    if (Number.isFinite(n) && n >= 0) {
                      patch({ idle_unload_minutes: Math.round(n) })
                    }
                  }}
                />
                <span className="text-xs text-ink-faint">minutes</span>
              </div>
            </label>
          )}
        </section>

        <section className="card space-y-4">
          <div>
            <h2 className="section-title">Download models</h2>
            <p className="mt-1 text-sm text-ink-muted">
              When a profile needs a model that isn’t installed yet.
            </p>
          </div>
          <ChoiceGroup
            value={(settings?.download_behavior ?? 'ask') as 'ask' | 'automatic'}
            onChange={(id) => patch({ download_behavior: id })}
            options={[
              { id: 'ask', label: 'Ask first' },
              { id: 'automatic', label: 'Automatic' },
            ]}
          />
        </section>

        <section className="card space-y-4">
          <div className="flex items-start justify-between gap-4">
            <div>
              <h2 className="section-title">Advanced mode</h2>
              <p className="mt-1 text-sm text-ink-muted">
                Show model IDs, API details, Profiles, logs, and other power-user controls.
              </p>
            </div>
            <Toggle
              label="Advanced mode"
              checked={advancedMode}
              disabled={busy}
              onChange={() => {
                const next = !advancedMode
                setAdvancedMode(next)
                patch({ advanced_mode: next })
              }}
            />
          </div>
        </section>
      </div>

      <div className="settings-group">
        <p className="settings-group-label">Network & access</p>
        <section className="card space-y-4">
          <div className="flex items-start justify-between gap-4">
            <div>
              <h2 className="section-title">Find other computers</h2>
              <p className="mt-1 text-sm text-ink-muted">
                Advertise this computer and look for other Yggdrasil installs on your network.
              </p>
            </div>
            <Toggle
              label="Find other computers"
              checked={settings?.discovery_enabled ?? true}
              disabled={busy}
              onChange={() =>
                patch({ discovery_enabled: !(settings?.discovery_enabled ?? true) })
              }
            />
          </div>
          {settings?.discovery_needs_restart ? (
            <div className="rounded-lg border border-warning/30 bg-warning/10 px-4 py-3 text-sm text-ink">
              <p>
                Networking was updated. Restart Yggdrasil so other computers can fully reach this
                one.
              </p>
              <button
                type="button"
                className="btn-primary mt-3 px-3 py-1.5 text-xs"
                onClick={async () => {
                  const ok = await quitDesktopForRestart()
                  if (!ok) {
                    window.alert(
                      'Quit Yggdrasil completely, then open it again to finish applying network settings.',
                    )
                  }
                }}
              >
                Restart now
              </button>
            </div>
          ) : (
            <p className="text-xs text-ink-faint">
              Status:{' '}
              <span className="font-medium text-ink">
                {(settings?.discovery_enabled ?? true) ? 'On' : 'Off'}
              </span>
            </p>
          )}
        </section>

        <section className="card space-y-3">
          <div>
            <h2 className="section-title">API access</h2>
            <p className="mt-1 text-sm text-ink-muted">
              How apps and other devices can talk to Yggdrasil.
            </p>
          </div>
          <dl className="space-y-2 text-sm">
            <div className="flex justify-between gap-3">
              <dt className="text-ink-muted">Local API</dt>
              <dd className="font-medium text-ink">On</dd>
            </div>
            <div className="flex justify-between gap-3">
              <dt className="text-ink-muted">LAN API</dt>
              <dd className="font-medium text-ink">
                {settings?.lan_api_enabled ? 'On' : 'Off'}
              </dd>
            </div>
          </dl>
          <Link to="/api-access" className="btn-secondary inline-block px-3 py-1.5 text-xs">
            Manage API access
          </Link>
        </section>

        <section className="card space-y-4">
          <div>
            <h2 className="section-title">Notifications</h2>
            <p className="mt-1 text-sm text-ink-muted">
              Alerts for long-running work and team connectivity.
            </p>
          </div>
          <div className="flex items-start justify-between gap-4">
            <div>
              <p className="text-sm font-medium text-ink">Notify when tasks finish</p>
            </div>
            <Toggle
              label="Notify when tasks finish"
              checked={settings?.notify_task_finish ?? true}
              disabled={busy}
              onChange={() =>
                patch({ notify_task_finish: !(settings?.notify_task_finish ?? true) })
              }
            />
          </div>
          <div className="flex items-start justify-between gap-4">
            <div>
              <p className="text-sm font-medium text-ink">
                Notify when a paired computer goes offline
              </p>
            </div>
            <Toggle
              label="Notify when a paired computer goes offline"
              checked={settings?.notify_peer_offline ?? true}
              disabled={busy}
              onChange={() =>
                patch({ notify_peer_offline: !(settings?.notify_peer_offline ?? true) })
              }
            />
          </div>
        </section>
      </div>

      <div className="settings-group">
        <p className="settings-group-label">Privacy & security</p>
        <ConnectedServices />
        <section className="card space-y-4">
          <div>
            <h2 className="section-title">Tool permissions</h2>
            <p className="mt-1 text-sm text-ink-muted">
              Default rules for what the AI may do on this computer.
            </p>
          </div>
          {(
            [
              { key: 'tool_terminal' as const, label: 'Terminal commands' },
              { key: 'tool_file_writes' as const, label: 'File writes' },
              { key: 'tool_git' as const, label: 'Git operations' },
            ] as const
          ).map((row) => {
            const value = (settings?.[row.key] ?? 'ask') as string
            return (
              <label key={row.key} className="block text-sm">
                <span className="text-ink-muted">{row.label}</span>
                <select
                  className="field mt-1 w-full"
                  value={value}
                  disabled={busy}
                  onChange={(e) => patch({ [row.key]: e.target.value } as SettingsPatch)}
                >
                  <option value="ask">Ask</option>
                  <option value="allow-for-session">Allow for session</option>
                  <option value="allow">Allow</option>
                  <option value="deny">Deny</option>
                </select>
              </label>
            )
          })}
        </section>

        <section className="card space-y-4">
          <div>
            <h2 className="section-title">History</h2>
            <p className="mt-1 text-sm text-ink-muted">
              Control what Yggdrasil keeps on this computer.
            </p>
          </div>
          <div className="flex items-start justify-between gap-4">
            <p className="text-sm font-medium text-ink">Save chat history</p>
            <Toggle
              label="Save chat history"
              checked={settings?.save_chat_history ?? true}
              disabled={busy}
              onChange={() =>
                patch({ save_chat_history: !(settings?.save_chat_history ?? true) })
              }
            />
          </div>
          <div className="flex items-start justify-between gap-4">
            <p className="text-sm font-medium text-ink">Save task history</p>
            <Toggle
              label="Save task history"
              checked={settings?.save_task_history ?? true}
              disabled={busy}
              onChange={() =>
                patch({ save_task_history: !(settings?.save_task_history ?? true) })
              }
            />
          </div>
          <button
            type="button"
            className="btn-secondary px-3 py-1.5 text-xs"
            disabled={clearHistoryMutation.isPending}
            onClick={() => {
              if (
                window.confirm(
                  'Delete all chat history on this computer? This cannot be undone.',
                )
              ) {
                clearHistoryMutation.mutate()
              }
            }}
          >
            {clearHistoryMutation.isPending ? 'Clearing…' : 'Clear chat history'}
          </button>
        </section>
      </div>

      <div className="settings-group">
        <p className="settings-group-label">Storage</p>
        <section className="card space-y-4">
          <h2 className="section-title">Data directories</h2>
          <div className="space-y-4">
            <PathRow label="Data" path={settings?.data_dir} openLabel={folderLabel} />
            <PathRow label="Models" path={settings?.models_dir} openLabel={folderLabel} />
            {advancedMode && (
              <>
                <PathRow
                  label="Runtimes"
                  path={settings?.runtimes_dir}
                  openLabel={folderLabel}
                />
                <PathRow label="Logs" path={settings?.logs_dir} openLabel={folderLabel} />
              </>
            )}
          </div>
        </section>

        <section className="card space-y-4">
          <div>
            <h2 className="section-title">Maximum model storage</h2>
            <p className="mt-1 text-sm text-ink-muted">
              Soft limit for downloaded models on this computer.
            </p>
          </div>
          <ChoiceGroup
            value={String(settings?.model_storage_limit_gb ?? 0)}
            onChange={(id) => patch({ model_storage_limit_gb: Number(id) })}
            options={[
              { id: '0', label: 'Unlimited' },
              { id: '50', label: '50 GB' },
              { id: '100', label: '100 GB' },
              { id: '250', label: '250 GB' },
            ]}
          />
        </section>
      </div>

      <div className="settings-group">
        <p className="settings-group-label">About</p>
        <section className="card space-y-4">
          <div className="flex items-center gap-4">
            <YggdrasilMark size={72} lore />
            <div className="min-w-0">
              <p className="font-display text-xl font-semibold tracking-tight text-ink">
                Yggdrasil
              </p>
              <p className="mt-0.5 text-sm text-ink-muted">Local AI control plane</p>
              <p className="mt-2 text-sm font-medium text-ink">
                {displayVersion(versionQuery.data?.version ?? healthQuery.data?.version) ||
                  'Version unavailable'}
              </p>
              {versionQuery.data?.commit &&
              versionQuery.data.commit !== 'unknown' &&
              versionQuery.data.commit.trim() !== '' ? (
                <p className="mt-0.5 font-mono text-xs text-ink-faint">
                  Build {versionQuery.data.commit.slice(0, 7)}
                </p>
              ) : null}
              {versionQuery.data?.source ? (
                <p className="mt-2 text-sm">
                  <a
                    href={versionQuery.data.source}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="text-primary underline-offset-2 hover:underline"
                  >
                    Corresponding source
                  </a>
                  {versionQuery.data.license ? (
                    <span className="text-ink-muted"> · {versionQuery.data.license}</span>
                  ) : null}
                </p>
              ) : null}
            </div>
          </div>
          <dl className="space-y-3 border-t border-line/50 pt-4 text-sm">
            <div>
              <dt className="text-xs uppercase tracking-wide text-ink-faint">This computer</dt>
              <dd className="font-medium text-ink">{settings?.node_name ?? 'Unavailable'}</dd>
            </div>
            <div>
              <dt className="text-xs uppercase tracking-wide text-ink-faint">Service</dt>
              <dd className="font-medium capitalize text-ink">
                {healthQuery.data?.status ?? 'Unavailable'}
              </dd>
            </div>
          </dl>
          <Link to="/diagnostics" className="btn-secondary inline-block">
            Open diagnostics
          </Link>
        </section>
      </div>

      <div className="settings-group">
        <p className="settings-group-label">Danger zone</p>
        <section className="card space-y-4">
          <div>
            <h2 className="section-title text-danger">Reset application</h2>
            <p className="mt-1 text-sm text-ink-muted">
              Restore defaults and restart onboarding. Clears chats, custom profiles, tasks, and
              API keys.
            </p>
          </div>

          {resetError && (
            <div className="rounded-lg bg-danger/10 px-4 py-3 text-sm text-danger">
              {resetError}
            </div>
          )}

          {!confirmReset ? (
            <button
              type="button"
              className="btn-danger"
              onClick={() => {
                setResetError(null)
                setDeleteModels(false)
                setConfirmReset(true)
              }}
            >
              Reset settings and app data…
            </button>
          ) : (
            <div className="space-y-3 rounded-xl bg-danger/10 p-4">
              <p className="text-sm text-danger">
                This cannot be undone. You will go through onboarding again.
                {!deleteModels
                  ? ' Downloaded models and runtimes will be kept.'
                  : ' Downloaded models will also be deleted.'}
              </p>
              <label className="flex items-start gap-2 text-sm text-ink">
                <input
                  type="checkbox"
                  className="mt-0.5 rounded border-line"
                  checked={deleteModels}
                  onChange={(e) => setDeleteModels(e.target.checked)}
                />
                <span>Also delete downloaded models</span>
              </label>
              <div className="flex flex-wrap gap-2">
                <button
                  type="button"
                  className="rounded-lg bg-danger px-4 py-2 text-sm font-medium text-[#EEF2F6] transition hover:bg-danger/90 disabled:opacity-50"
                  disabled={resetMutation.isPending}
                  onClick={() => resetMutation.mutate()}
                >
                  {resetMutation.isPending ? 'Resetting…' : 'Yes, reset'}
                </button>
                <button
                  type="button"
                  className="btn-secondary"
                  disabled={resetMutation.isPending}
                  onClick={() => setConfirmReset(false)}
                >
                  Cancel
                </button>
              </div>
            </div>
          )}
        </section>
      </div>
    </div>
  )
}
