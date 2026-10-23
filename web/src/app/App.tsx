import type { ReactNode } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { RequireRole } from '@/components/auth/RequireRole'
import { PortalPage } from '@/features/portal/PortalPage'
import { PortalsPage } from '@/features/portals/PortalsPage'
import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'
import { LifecycleHost } from '@/components/LifecycleHost'
import { NotificationHost } from '@/components/NotificationHost'
import { ServiceBootGate } from '@/components/ServiceBootGate'
import { LanguageSync } from '@/components/LanguageSync'
import { ThemeSync } from '@/components/ThemeSync'
import { AppLayout } from '@/components/layout/AppLayout'
import { ScreenshotMode } from '@/app/ScreenshotMode'
import { readScreenshotLaunch } from '@/lib/screenshotMode'
import { AutomationsPage } from '@/features/automations/AutomationsPage'
import { ApiAccessPage } from '@/features/api-access/ApiAccessPage'
import { ChatPage } from '@/features/chat/ChatPage'
import { DiagnosticsPage } from '@/features/diagnostics/DiagnosticsPage'
import { ModelsPage } from '@/features/models/ModelsPage'
import { NodesPage } from '@/features/nodes/NodesPage'
import { OnboardingPage } from '@/features/onboarding/OnboardingPage'
import { PerformancePage } from '@/features/performance/PerformancePage'
import { ProfilesPage } from '@/features/profiles/ProfilesPage'
import { SettingsPage } from '@/features/settings/SettingsPage'
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

function OnboardingGate({ children }: { children: ReactNode }) {
  const onboardingComplete = useUIStore((s) => s.onboardingComplete)
  if (readScreenshotLaunch()?.enabled || onboardingComplete) {
    return <>{children}</>
  }
  return <Navigate to="/onboarding" replace />
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
                <Route path="automations" element={<RequireRole min="member"><AutomationsPage /></RequireRole>} />
                <Route path="models" element={<RequireRole min="admin"><ModelsPage /></RequireRole>} />
                <Route path="train" element={<RequireRole min="admin"><TrainPage /></RequireRole>} />
                <Route path="knowledge" element={<RequireRole min="admin"><KnowledgePage /></RequireRole>} />
                <Route path="memory" element={<RequireRole min="member"><MemoryPage /></RequireRole>} />
                <Route path="news" element={<RequireRole min="member"><NewsPage /></RequireRole>} />
                <Route path="profiles" element={<RequireRole min="admin"><ProfilesPage /></RequireRole>} />
                <Route path="tools" element={<RequireRole min="admin"><ToolsPage /></RequireRole>} />
                <Route path="nodes" element={<RequireRole min="admin"><NodesPage /></RequireRole>} />
                <Route path="people" element={<PeoplePage />} />
                <Route path="portals" element={<RequireRole min="admin"><PortalsPage /></RequireRole>} />
                <Route path="api-access" element={<RequireRole min="admin"><ApiAccessPage /></RequireRole>} />
                <Route path="diagnostics" element={<RequireRole min="admin"><DiagnosticsPage /></RequireRole>} />
                <Route path="performance" element={<RequireRole min="admin"><PerformancePage /></RequireRole>} />
                <Route path="settings" element={<SettingsPage />} />
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
