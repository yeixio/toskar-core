import { useEffect, useRef } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { useRole } from '@/lib/role'
import { notifyDesktopBackgroundMode } from '@/lib/desktopBridge'
import { readScreenshotLaunch } from '@/lib/screenshotMode'

/**
 * Tells the desktop app whether closing the window keeps Toskar running.
 * Toskar turns the setting on when the first schedule is saved; after the
 * person sets it themselves, their choice stands (#1209).
 */
export function ScheduleBackgroundSync() {
  const screenshot = readScreenshotLaunch()?.enabled ?? false
  const told = useRef<boolean | null>(null)
  // Keeping the service running is an Admin's setting (#203).
  const admin = useRole().atLeast('admin')
  const settings = useQuery({
    queryKey: ['settings'],
    queryFn: () => api.getSettings(),
    enabled: !screenshot && admin,
    retry: false,
  })
  const on = settings.data?.keep_running_in_background

  useEffect(() => {
    if (on === undefined || told.current === on) return
    told.current = on
    void notifyDesktopBackgroundMode(on)
  }, [on])

  return null
}
