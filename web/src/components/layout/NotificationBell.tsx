import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'
import i18n from '@/i18n'
import { api } from '@/lib/api'
import { subscribeEvents } from '@/lib/events'
import type { AppNotification, NotificationCategory, NotificationList } from '@/types/api'
import { CATEGORIES, categoryLabel, deliveryNote, noticeText } from '@/features/settings/notificationLabels'
import { formatDate, formatRelativeTime } from '@/i18n/format'
import { LoadError } from '@/components/ui/LoadError'

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
  if (seconds < 60) return i18n.t('notifications:bell.justNow')
  if (seconds < 24 * 3600) return formatRelativeTime(iso, new Date(), 'narrow')
  return formatDate(iso)
}

/**
 * Gjallarhorn's bell: the unread count, and the notification center it opens.
 * Notifications come from the daemon, so ones that arrived while the app was
 * closed are here too. It refreshes when the daemon announces a new one.
 */
export function NotificationBell() {
  const { t } = useTranslation('notifications')
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const [open, setOpen] = useState(false)
  const [category, setCategory] = useState<'' | NotificationCategory>('')
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
  const all = query.data?.notifications ?? []
  const present = CATEGORIES.filter((c) => all.some((n) => n.category === c))
  const notifications = category ? all.filter((n) => n.category === category) : all
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
      const target = e.target as Node
      if (!panel.current?.contains(target) && !button.current?.contains(target)) setOpen(false)
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
        aria-label={unread > 0 ? t('bell.labelUnread', { count: unread }) : t('bell.label')}
        aria-expanded={open}
        aria-haspopup="dialog"
        onClick={() => setOpen((v) => !v)}
      >
        <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" aria-hidden>
          <path d="M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9" strokeLinecap="round" strokeLinejoin="round" />
          <path d="M10.3 21a1.94 1.94 0 0 0 3.4 0" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
        {unread > 0 && (
          <span className="absolute -end-0.5 -top-0.5 min-w-4 rounded-full bg-heimdall px-1 text-center text-[10px] font-semibold leading-4 text-primary-fg tabular-nums">
            {unread > 99 ? '99+' : unread}
          </span>
        )}
      </button>

      {open &&
        createPortal(
          <div
            ref={panel}
            role="dialog"
            aria-label={t('bell.title')}
            className="fixed z-50 flex max-h-[70vh] flex-col overflow-hidden rounded-xl border border-line bg-surface shadow-lg"
            style={{ left: place?.left ?? EDGE, top: place?.top ?? EDGE, width: Math.min(WIDTH, window.innerWidth - EDGE * 2) }}
          >
            <div className="flex items-center justify-between gap-2 border-b border-line/60 px-4 py-3">
              <div>
                <p className="text-sm font-semibold text-ink">{t('bell.title')}</p>
                <p className="text-[11px] text-ink-faint">Gjallarhorn</p>
              </div>
              {unread > 0 && (
                <button type="button" className="text-xs text-primary hover:underline" onClick={() => markRead.mutate([])}>
                  {t('bell.markAllRead')}
                </button>
              )}
            </div>
            {present.length > 1 && (
              <div className="flex flex-wrap gap-1 border-b border-line/60 px-3 py-2" role="group" aria-label={t('bell.filter')}>
                {(['', ...present] as ('' | NotificationCategory)[]).map((c) => (
                  <button
                    key={c || 'all'}
                    type="button"
                    aria-pressed={category === c}
                    className={[
                      'rounded-full px-2 py-0.5 text-[11px]',
                      category === c ? 'bg-primary-soft text-ink' : 'text-ink-muted hover:bg-raised',
                    ].join(' ')}
                    onClick={() => setCategory(c)}
                  >
                    {c ? categoryLabel(c) : t('bell.all')}
                  </button>
                ))}
              </div>
            )}
            <ul className="min-h-0 flex-1 overflow-y-auto">
              {query.isError && !query.data ? (
                <li className="px-4 py-6">
                  <LoadError compact error={query.error} onRetry={() => void query.refetch()} retrying={query.isFetching} />
                </li>
              ) : query.isLoading ? (
                <li className="px-4 py-8 text-center text-sm text-ink-faint">…</li>
              ) : notifications.length === 0 ? (
                <li className="px-4 py-8 text-center text-sm text-ink-faint">
                  {t('bell.empty')}
                </li>
              ) : null}
              {notifications.map((n) => {
                const text = noticeText(n)
                return (
                  <li key={n.id} className={['group flex gap-3 border-b border-line/40 px-4 py-3 last:border-b-0', n.read_at ? '' : 'bg-primary-soft/40'].join(' ')}>
                    <span className={['mt-1.5 h-2 w-2 shrink-0 rounded-full', SEVERITY_DOT[n.severity] ?? 'bg-info'].join(' ')} aria-hidden />
                    <button type="button" className="min-w-0 flex-1 text-start" onClick={() => openItem(n)}>
                      <p className={['text-sm text-ink', n.read_at ? '' : 'font-semibold'].join(' ')}>
                        {text.title}
                        {(n.repeat_count ?? 1) > 1 && <span className="font-normal text-ink-muted"> · {t('bell.times', { count: n.repeat_count })}</span>}
                      </p>
                      {text.body && <p className="mt-0.5 line-clamp-3 text-xs text-ink-muted">{text.body}</p>}
                      <p className="mt-1 text-[11px] text-ink-faint">
                        {ago(n.created_at)}
                        {deliveryNote(n) && <span> · {deliveryNote(n)}</span>}
                        {!n.read_at && <span className="sr-only"> · {t('bell.unread')}</span>}
                      </p>
                    </button>
                    <button
                      type="button"
                      className="self-start rounded p-1 text-ink-faint opacity-60 hover:bg-raised hover:text-ink group-hover:opacity-100"
                      aria-label={t('bell.dismiss', { title: text.title })}
                      onClick={() => dismiss.mutate(n.id)}
                    >
                      ×
                    </button>
                  </li>
                )
              })}
            </ul>
          </div>,
          document.body,
        )}
    </>
  )
}
