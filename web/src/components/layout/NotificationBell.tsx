import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { api } from '@/lib/api'
import { subscribeEvents } from '@/lib/events'
import type { AppNotification, NotificationList } from '@/types/api'

const KEY = ['notifications'] as const
const WIDTH = 352
const EDGE = 8

const SEVERITY_DOT: Record<AppNotification['severity'], string> = {
  info: 'bg-info',
  success: 'bg-success',
  warning: 'bg-warning',
  error: 'bg-danger',
}

function ago(iso: string): string {
  const seconds = Math.max(0, Math.round((Date.now() - new Date(iso).getTime()) / 1000))
  if (seconds < 60) return 'just now'
  const minutes = Math.round(seconds / 60)
  if (minutes < 60) return `${minutes} min ago`
  const hours = Math.round(minutes / 60)
  if (hours < 24) return `${hours} h ago`
  return new Date(iso).toLocaleDateString()
}

/**
 * Gjallarhorn's bell: the unread count, and the notification center it opens.
 * Notifications come from the daemon, so ones that arrived while the app was
 * closed are here too. It refreshes when the daemon announces a new one.
 */
export function NotificationBell() {
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const [open, setOpen] = useState(false)
  const [place, setPlace] = useState<{ left: number; top: number } | null>(null)
  const button = useRef<HTMLButtonElement>(null)
  const panel = useRef<HTMLDivElement>(null)

  const query = useQuery({
    queryKey: KEY,
    queryFn: () => api.listNotifications(),
    staleTime: 15_000,
    refetchInterval: 60_000,
    retry: false,
  })
  const notifications = query.data?.notifications ?? []
  const unread = query.data?.unread ?? 0

  useEffect(
    () =>
      subscribeEvents({
        onEvent: (event) => {
          if (event.type === 'notification.created') void queryClient.invalidateQueries({ queryKey: KEY })
        },
      }),
    [queryClient],
  )

  const markRead = useMutation({
    mutationFn: (ids: string[]) => api.markNotificationsRead(ids),
    onMutate: (ids) => {
      const now = new Date().toISOString()
      queryClient.setQueryData<NotificationList>(KEY, (data) =>
        data && {
          notifications: data.notifications.map((n) =>
            ids.length === 0 || ids.includes(n.id) ? { ...n, read_at: n.read_at ?? now } : n,
          ),
          unread: ids.length === 0 ? 0 : Math.max(0, data.unread - data.notifications.filter((n) => ids.includes(n.id) && !n.read_at).length),
        },
      )
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: KEY }),
  })

  const dismiss = useMutation({
    mutationFn: (id: string) => api.dismissNotification(id),
    onMutate: (id) => {
      queryClient.setQueryData<NotificationList>(KEY, (data) => {
        if (!data) return data
        const gone = data.notifications.find((n) => n.id === id)
        return {
          notifications: data.notifications.filter((n) => n.id !== id),
          unread: gone && !gone.read_at ? Math.max(0, data.unread - 1) : data.unread,
        }
      })
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: KEY }),
  })

  useEffect(() => {
    if (!open) return
    const r = button.current?.getBoundingClientRect()
    if (r) {
      const width = Math.min(WIDTH, window.innerWidth - EDGE * 2)
      setPlace({
        left: Math.max(EDGE, Math.min(r.right + EDGE, window.innerWidth - width - EDGE)),
        top: Math.max(EDGE, r.top),
      })
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        setOpen(false)
        button.current?.focus()
      }
    }
    const onDown = (e: MouseEvent) => {
      const t = e.target as Node
      if (!panel.current?.contains(t) && !button.current?.contains(t)) setOpen(false)
    }
    document.addEventListener('keydown', onKey)
    document.addEventListener('mousedown', onDown)
    return () => {
      document.removeEventListener('keydown', onKey)
      document.removeEventListener('mousedown', onDown)
    }
  }, [open])

  const openItem = (n: AppNotification) => {
    if (!n.read_at) markRead.mutate([n.id])
    if (n.link) {
      setOpen(false)
      navigate(n.link)
    }
  }

  return (
    <>
      <button
        ref={button}
        type="button"
        className="relative rounded-md p-1.5 text-ink-muted transition-colors hover:bg-raised hover:text-ink"
        aria-label={unread > 0 ? `Notifications, ${unread} unread` : 'Notifications'}
        aria-expanded={open}
        aria-haspopup="dialog"
        onClick={() => setOpen((v) => !v)}
      >
        <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" aria-hidden>
          <path d="M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9" strokeLinecap="round" strokeLinejoin="round" />
          <path d="M10.3 21a1.94 1.94 0 0 0 3.4 0" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
        {unread > 0 && (
          <span className="absolute -right-0.5 -top-0.5 min-w-4 rounded-full bg-heimdall px-1 text-center text-[10px] font-semibold leading-4 text-primary-fg tabular-nums">
            {unread > 99 ? '99+' : unread}
          </span>
        )}
      </button>

      {open &&
        createPortal(
          <div
            ref={panel}
            role="dialog"
            aria-label="Notifications"
            className="fixed z-50 flex max-h-[70vh] flex-col overflow-hidden rounded-xl border border-line bg-surface shadow-lg"
            style={{ left: place?.left ?? EDGE, top: place?.top ?? EDGE, width: Math.min(WIDTH, window.innerWidth - EDGE * 2) }}
          >
            <div className="flex items-center justify-between gap-2 border-b border-line/60 px-4 py-3">
              <div>
                <p className="text-sm font-semibold text-ink">Notifications</p>
                <p className="text-[11px] text-ink-faint">Gjallarhorn</p>
              </div>
              {unread > 0 && (
                <button type="button" className="text-xs text-primary hover:underline" onClick={() => markRead.mutate([])}>
                  Mark all read
                </button>
              )}
            </div>
            <ul className="min-h-0 flex-1 overflow-y-auto">
              {notifications.length === 0 && (
                <li className="px-4 py-8 text-center text-sm text-ink-faint">
                  Nothing yet. Finished automations, downloads, and anything that needs you will show up here.
                </li>
              )}
              {notifications.map((n) => (
                <li key={n.id} className={['group flex gap-3 border-b border-line/40 px-4 py-3 last:border-b-0', n.read_at ? '' : 'bg-primary-soft/40'].join(' ')}>
                  <span className={['mt-1.5 h-2 w-2 shrink-0 rounded-full', SEVERITY_DOT[n.severity] ?? 'bg-info'].join(' ')} aria-hidden />
                  <button type="button" className="min-w-0 flex-1 text-left" onClick={() => openItem(n)}>
                    <p className={['text-sm text-ink', n.read_at ? '' : 'font-semibold'].join(' ')}>{n.title}</p>
                    {n.body && <p className="mt-0.5 line-clamp-3 text-xs text-ink-muted">{n.body}</p>}
                    <p className="mt-1 text-[11px] text-ink-faint">
                      {ago(n.created_at)}
                      {!n.read_at && <span className="sr-only"> · unread</span>}
                    </p>
                  </button>
                  <button
                    type="button"
                    className="self-start rounded p-1 text-ink-faint opacity-60 hover:bg-raised hover:text-ink group-hover:opacity-100"
                    aria-label={`Dismiss ${n.title}`}
                    onClick={() => dismiss.mutate(n.id)}
                  >
                    ×
                  </button>
                </li>
              ))}
            </ul>
          </div>,
          document.body,
        )}
    </>
  )
}
