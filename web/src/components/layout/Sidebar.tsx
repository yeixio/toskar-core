import { useQuery } from '@tanstack/react-query'
import { NavLink } from 'react-router-dom'
import { api } from '@/lib/api'
import { displayVersion } from '@/lib/appVersion'
import { useUIStore } from '@/stores/uiStore'

const mainNav = [
  { to: '/chat', label: 'Chat' },
  { to: '/models', label: 'Models' },
  { to: '/nodes', label: 'Computers' },
] as const

const systemNav = [
  { to: '/performance', label: 'Performance' },
  { to: '/diagnostics', label: 'Diagnostics' },
  { to: '/profiles', label: 'Profiles', advanced: true },
  { to: '/tools', label: 'Tools', advanced: true },
  { to: '/api-access', label: 'API Access', advanced: true },
] as const

export function Sidebar() {
  const advancedMode = useUIStore((s) => s.advancedMode)
  const healthQuery = useQuery({
    queryKey: ['health'],
    queryFn: async () => {
      const health = await api.getHealth()
      if (!health || health.status !== 'ok') {
        throw new Error('Service not ready')
      }
      return health
    },
    refetchInterval: (query) =>
      query.state.data?.status === 'ok' ? 15_000 : 1_000,
    retry: true,
    retryDelay: (attempt) => Math.min(400 + attempt * 250, 2000),
  })
  const nodesQuery = useQuery({
    queryKey: ['nodes'],
    queryFn: () => api.getNodes(),
    refetchInterval: 30_000,
    retry: false,
    enabled: healthQuery.data?.status === 'ok',
  })
  const modelsQuery = useQuery({
    queryKey: ['models'],
    queryFn: () => api.getModels(),
    refetchInterval: 60_000,
    retry: false,
    enabled: healthQuery.data?.status === 'ok',
  })

  const serviceOk = healthQuery.data?.status === 'ok'
  const healthPending =
    !serviceOk &&
    (healthQuery.isPending || healthQuery.isFetching || healthQuery.isLoading)
  const nodeCount = nodesQuery.data?.length ?? 0
  const hasModel = (modelsQuery.data ?? []).some(
    (m) => m.installed || (m.installed_on?.length ?? 0) > 0,
  )

  const runningVersion = displayVersion(healthQuery.data?.version)

  const statusLabel = !serviceOk
    ? healthPending
      ? 'Starting…'
      : 'Service unavailable'
    : !hasModel
      ? 'No model'
      : 'Ready'

  const statusTone = !serviceOk
    ? healthPending
      ? 'bg-info/15 text-info'
      : 'bg-warning/15 text-warning'
    : !hasModel
      ? 'bg-warning/15 text-warning'
      : 'bg-success/15 text-success'

  const statusDot = !serviceOk
    ? healthPending
      ? 'bg-info animate-pulse'
      : 'bg-warning animate-pulse'
    : !hasModel
      ? 'bg-warning'
      : 'bg-success'

  const systemItems = systemNav.filter(
    (item) => !('advanced' in item && item.advanced) || advancedMode,
  )

  return (
    <aside className="app-sidebar flex h-full min-h-0 w-[15.5rem] shrink-0 flex-col overflow-hidden border-r border-line/60 bg-sidebar">
      <div className="px-4 pb-3 pt-4">
        <div className="flex items-center gap-2.5">
          <a href="/" className="brand flex min-w-0 flex-1 items-center gap-2.5 no-underline">
            <img
              src="/yggdrasil-mark.png"
              alt=""
              width={36}
              height={36}
              className="h-9 w-9 shrink-0 object-contain"
              decoding="async"
            />
            <div className="min-w-0 leading-none">
              <div className="flex items-center gap-2">
                <p className="font-display text-xl font-semibold tracking-tight text-ink">
                  Yggdrasil
                </p>
                <span
                  className={['status-chip shrink-0', statusTone].join(' ')}
                  title={
                    !serviceOk
                      ? 'The local Yggdrasil service is not responding.'
                      : !hasModel
                        ? 'Install a model in Models to start chatting.'
                        : 'Local service is ready.'
                  }
                >
                  <span className={['h-1.5 w-1.5 rounded-full', statusDot].join(' ')} aria-hidden />
                  {statusLabel}
                </span>
              </div>
              <p className="mt-1 whitespace-nowrap text-[11px] leading-none text-ink-faint">
                {runningVersion || 'Local AI control plane'}
              </p>
            </div>
          </a>
        </div>
      </div>

      <nav className="flex flex-1 flex-col gap-4 overflow-y-auto px-2.5 pb-3" aria-label="Main">
        <div>
          <p className="label-caps mb-1 px-2.5">Main</p>
          <div className="flex flex-col gap-0.5">
            {mainNav.map(({ to, label }) => (
              <NavLink
                key={to}
                to={to}
                className={({ isActive }) =>
                  ['nav-link', isActive ? 'nav-link-active' : ''].filter(Boolean).join(' ')
                }
              >
                {label}
              </NavLink>
            ))}
          </div>
        </div>

        <div>
          <p className="label-caps mb-1 px-2.5">System</p>
          <div className="flex flex-col gap-0.5">
            {systemItems.map(({ to, label }) => (
              <NavLink
                key={to}
                to={to}
                className={({ isActive }) =>
                  ['nav-link', isActive ? 'nav-link-active' : ''].filter(Boolean).join(' ')
                }
              >
                {label}
              </NavLink>
            ))}
            <NavLink
              to="/settings"
              className={({ isActive }) =>
                ['nav-link', isActive ? 'nav-link-active' : ''].filter(Boolean).join(' ')
              }
            >
              Settings
            </NavLink>
          </div>
        </div>
      </nav>

      <div className="mt-auto space-y-1.5 border-t border-line/50 px-4 py-3">
        <p className="label-caps mb-2 text-[10px] text-ink-faint">System</p>
        <div
          className="flex items-center justify-between gap-2 text-[11px] text-ink-faint"
          title="Computer discovery and networking"
        >
          <span className="flex items-center gap-1.5">
            <span className="h-1 w-1 rounded-full bg-bifrost" aria-hidden />
            Bifrost
          </span>
          <span className="tabular-nums">
            {nodeCount === 0
              ? 'No computers'
              : `${nodeCount} ${nodeCount === 1 ? 'computer' : 'computers'} connected`}
          </span>
        </div>
        <div
          className="flex items-center justify-between gap-2 text-[11px] text-ink-faint"
          title="Norn schedules work across your computers"
        >
          <span className="flex items-center gap-1.5">
            <span className="h-1 w-1 rounded-full bg-norn" aria-hidden />
            Norn
          </span>
          <span>Automatic</span>
        </div>
        <div
          className="flex items-center justify-between gap-2 text-[11px] text-ink-faint"
          title="Local knowledge and memory"
        >
          <span className="flex items-center gap-1.5">
            <span className="h-1 w-1 rounded-full bg-mimir" aria-hidden />
            Mimir
          </span>
          <span>Ready</span>
        </div>
      </div>
    </aside>
  )
}
