import { useEffect, type ReactNode } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { RequireRole } from '@/components/auth/RequireRole'
import { PortalPage } from '@/features/portal/PortalPage'
import { PortalsPage } from '@/features/portals/PortalsPage'
import { BrowserRouter, Navigate, Route, Routes, useLocation } from 'react-router-dom'
import { MovedPage } from '@/app/MovedPage'
import { FeaturesPage } from '@/features/settings/FeaturesPage'
import { LifecycleHost } from '@/components/LifecycleHost'
import { NotificationHost } from '@/components/NotificationHost'
import { ServiceBootGate } from '@/components/ServiceBootGate'
import { LanguageSync } from '@/components/LanguageSync'
import { ThemeSync } from '@/components/ThemeSync'
import { AppLayout } from '@/components/layout/AppLayout'
import { ScreenshotMode } from '@/app/ScreenshotMode'
import { desktopFreshInstall } from '@/lib/desktopBridge'
import { readScreenshotLaunch } from '@/lib/screenshotMode'
import { AutomationsPage } from '@/features/automations/AutomationsPage'
import { ApiAccessPage } from '@/features/api-access/ApiAccessPage'
import { ChatPage } from '@/features/chat/ChatPage'
import { ProjectPage } from '@/features/projects/ProjectPage'
import { DiagnosticsPage } from '@/features/diagnostics/DiagnosticsPage'
import { ModelsPage } from '@/features/models/ModelsPage'
import { NodesPage } from '@/features/nodes/NodesPage'
import { OnboardingPage } from '@/features/onboarding/OnboardingPage'
import { PerformancePage } from '@/features/performance/PerformancePage'
import { ProfilesPage } from '@/features/profiles/ProfilesPage'
import { SettingsPage } from '@/features/settings/SettingsPage'
import { SettingsLayout } from '@/features/settings/SettingsLayout'
import { movedPages } from '@/features/settings/settingsNav'
import { ShareWithAppsPage } from '@/features/api-access/ShareWithApps'
import { ToolsPage } from '@/features/tools/ToolsPage'
import { KnowledgePage } from '@/features/knowledge/KnowledgePage'
import { MemoryPage } from '@/features/memory/MemoryPage'
import { NewsPage } from '@/features/news/NewsPage'
import { TrainPage } from '@/features/train/TrainPage'
import { InvitePage } from '@/features/people/InvitePage'
import { PeoplePage } from '@/features/people/PeoplePage'
import { useUIStore } from '@/stores/uiStore'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      refetchOnWindowFocus: false,
      // One retry, not three: a failed request shows its error with Try
      // again in about a second, rather than a spinner for seven.
      retry: 1,
    },
  },
})

/**
 * On a desktop app's new install, offers setup again even when the window
 * remembers it from an earlier one (toskar-apps#455).
 */
function FreshInstallSync() {
  // The shell answers true only once, so the answer is kept even if this
  // unmounts first.
  useEffect(() => {
    void desktopFreshInstall().then((fresh) => {
      if (fresh) useUIStore.getState().setOnboardingComplete(false)
    })
  }, [])
  return null
}

function OnboardingGate({ children }: { children: ReactNode }) {
  const onboardingComplete = useUIStore((s) => s.onboardingComplete)
  const location = useLocation()
  if (readScreenshotLaunch()?.enabled || onboardingComplete) {
    return <>{children}</>
  }
  // The page asked for is where Skip goes, not always Chat (#1266).
  const next = location.pathname + location.search
  const ask = location.pathname === '/' || location.pathname === '/chat' ? '' : `?next=${encodeURIComponent(next)}`
  return <Navigate to={`/onboarding${ask}`} replace />
}

/**
 * The chat portal at /p/<portal> (#205), or null. A portal's page is its
 * own small app, without the app's hosts around it, which would ask the
 * service things a portal's visitor may not.
 */
function portalSlug(): string | null {
  if (typeof window === 'undefined') return null
  const match = /^\/p\/([a-z0-9-]{2,40})\/?$/i.exec(window.location.pathname)
  return match ? match[1].toLowerCase() : null
}

export function App() {
  const portal = portalSlug()
  if (portal) {
    return (
      <QueryClientProvider client={queryClient}>
        <PortalPage slug={portal} />
      </QueryClientProvider>
    )
  }
  return (
    <QueryClientProvider client={queryClient}>
      <ThemeSync />
      <LanguageSync />
      <LifecycleHost>
        <NotificationHost />
        <BrowserRouter>
          <ScreenshotMode />
          <FreshInstallSync />
          <ServiceBootGate>
            <Routes>
              {/* A one-time sign-in link answers before sign-in (#206). */}
              <Route path="/invite/:token" element={<InvitePage />} />
              <Route path="/onboarding" element={<OnboardingPage />} />
              <Route
                element={
                  <OnboardingGate>
                    <AppLayout />
                  </OnboardingGate>
                }
              >
                <Route index element={<Navigate to="/chat" replace />} />
                <Route path="chat" element={<ChatPage />} />
                {/* A project's page (#1136); the projects themselves are listed in Chat's history. */}
                <Route path="projects/:id" element={<ProjectPage />} />
                <Route path="projects" element={<Navigate to="/chat" replace />} />
                <Route path="automations" element={<RequireRole min="member"><AutomationsPage /></RequireRole>} />
                <Route path="models" element={<RequireRole min="admin"><ModelsPage /></RequireRole>} />
                <Route path="models/train" element={<RequireRole min="admin"><TrainPage /></RequireRole>} />
                <Route path="knowledge" element={<RequireRole min="admin"><KnowledgePage /></RequireRole>} />
                <Route path="news" element={<RequireRole min="member"><NewsPage /></RequireRole>} />
                <Route path="profiles" element={<RequireRole min="admin"><ProfilesPage /></RequireRole>} />
                {/* Settings' categories (#1374, #1321, docs/settings-inventory.md). */}
                <Route path="settings" element={<SettingsLayout />}>
                  <Route index element={<SettingsPage section="account" />} />
                  <Route path="account" element={<SettingsPage section="account" />} />
                  <Route path="general" element={<SettingsPage section="general" />} />
                  <Route path="appearance" element={<SettingsPage section="appearance" />} />
                  <Route path="personalization" element={<SettingsPage section="personalization" />} />
                  <Route path="memory" element={<RequireRole min="member"><MemoryPage /></RequireRole>} />
                  <Route path="notifications" element={<RequireRole min="admin"><SettingsPage section="notifications" /></RequireRole>} />
                  <Route path="voice" element={<RequireRole min="admin"><SettingsPage section="voice" /></RequireRole>} />
                  <Route path="features" element={<RequireRole min="admin"><FeaturesPage /></RequireRole>} />
                  <Route path="models" element={<RequireRole min="admin"><SettingsPage section="models" /></RequireRole>} />
                  <Route path="history" element={<RequireRole min="admin"><SettingsPage section="history" /></RequireRole>} />
                  <Route path="computers" element={<RequireRole min="admin"><NodesPage /></RequireRole>} />
                  <Route path="network" element={<RequireRole min="admin"><ApiAccessPage part="network" /></RequireRole>} />
                  <Route path="people" element={<PeoplePage />} />
                  <Route path="portals" element={<RequireRole min="admin"><PortalsPage /></RequireRole>} />
                  <Route path="tools" element={<RequireRole min="admin"><ToolsPage /></RequireRole>} />
                  <Route path="apps" element={<RequireRole min="admin"><ShareWithAppsPage /></RequireRole>} />
                  <Route path="privacy" element={<RequireRole min="admin"><SettingsPage section="privacy" /></RequireRole>} />
                  <Route path="advanced" element={<RequireRole min="admin"><SettingsPage section="advanced" /></RequireRole>} />
                  <Route path="performance" element={<RequireRole min="admin"><PerformancePage /></RequireRole>} />
                  <Route path="diagnostics" element={<RequireRole min="admin"><DiagnosticsPage /></RequireRole>} />
                  <Route path="developer" element={<RequireRole min="admin"><ApiAccessPage part="developer" /></RequireRole>} />
                  <Route path="about" element={<RequireRole min="admin"><SettingsPage section="about" /></RequireRole>} />
                  <Route path="*" element={<Navigate to="/settings" replace />} />
                </Route>
                {/* Pages that moved into Settings keep their old links (#1374). */}
                {Object.entries(movedPages).map(([from, to]) => (
                  <Route key={from} path={from.slice(1)} element={<MovedPage to={to} />} />
                ))}
                {/* The old "coming soon" Orchestrators page; orchestration is chosen per profile. */}
                <Route path="orchestrators" element={<Navigate to="/profiles" replace />} />
              </Route>
              <Route path="*" element={<Navigate to="/chat" replace />} />
            </Routes>
          </ServiceBootGate>
        </BrowserRouter>
      </LifecycleHost>
    </QueryClientProvider>
  )
}
