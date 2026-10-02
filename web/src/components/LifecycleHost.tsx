import { useEffect, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { Ratatoskr } from '@/components/ui/Ratatoskr'
import { onDesktopLifecycle, type DesktopLifecycleEvent } from '@/lib/desktopBridge'

/**
 * Listens for Wails shell lifecycle events (quit / background) and shows
 * a blocking overlay while the local service is being stopped.
 */
export function LifecycleHost({ children }: { children: ReactNode }) {
  const { t } = useTranslation('desktop')
  const [event, setEvent] = useState<DesktopLifecycleEvent | null>(null)

  useEffect(() => onDesktopLifecycle(setEvent), [])

  return (
    <div className="h-full min-h-0 min-w-0 overflow-hidden">
      {children}
      {event?.state === 'stopping' ? (
        <div
          className="fixed inset-0 z-[100] flex flex-col items-center justify-center bg-canvas/92 px-6 backdrop-blur-sm"
          role="status"
          aria-live="assertive"
          aria-busy="true"
        >
          <Ratatoskr state="sleep" size={96} />
          <p className="mt-4 font-display text-xl font-semibold tracking-tight text-ink">
            {t('lifecycle.closingTitle')}
          </p>
          <p className="mt-2 max-w-sm text-center text-sm text-ink-muted">
            {t('lifecycle.stopping')}
          </p>
          <span
            className="mt-6 inline-block h-5 w-5 animate-spin rounded-full border-2 border-line border-t-primary"
            aria-hidden
          />
        </div>
      ) : null}
    </div>
  )
}
