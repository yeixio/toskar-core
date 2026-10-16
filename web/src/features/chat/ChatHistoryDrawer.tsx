import { useEffect, useMemo, useState, type KeyboardEvent, type MouseEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { useDialog } from '@/lib/useDialog'
import { LoadingSpinner } from '@/components/ui/LoadingSpinner'
import type { Conversation } from '@/types/api'
import { rovingKeyDown, useMenu } from '@/lib/roving'
import { searchItems } from '@/lib/search'
import { LoadError } from '@/components/ui/LoadError'

const PIN_MIN_WIDTH = 1100

export function useCanPinChatHistory(): boolean {
  const [canPin, setCanPin] = useState(() =>
    typeof window !== 'undefined' ? window.innerWidth >= PIN_MIN_WIDTH : true,
  )

  useEffect(() => {
    const mq = window.matchMedia(`(min-width: ${PIN_MIN_WIDTH}px)`)
    const sync = () => setCanPin(mq.matches)
    sync()
    mq.addEventListener('change', sync)
    return () => mq.removeEventListener('change', sync)
  }, [])

  return canPin
}

// Groups, in order; their names are chat:history.groups.<group> in the catalog.
type Group = 'pinned' | 'today' | 'yesterday' | 'week' | 'month' | 'older'

function recencyGroup(iso: string): Group {
  const d = new Date(iso)
  const now = new Date()
  const startToday = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  const startYesterday = new Date(startToday)
  startYesterday.setDate(startYesterday.getDate() - 1)
  const start7 = new Date(startToday)
  start7.setDate(start7.getDate() - 7)
  const start30 = new Date(startToday)
  start30.setDate(start30.getDate() - 30)

  if (d >= startToday) return 'today'
  if (d >= startYesterday) return 'yesterday'
  if (d >= start7) return 'week'
  if (d >= start30) return 'month'
  return 'older'
}

const GROUP_ORDER: Group[] = ['pinned', 'today', 'yesterday', 'week', 'month', 'older']

function groupConversations(
  items: Conversation[],
  pinnedIds: string[],
): { label: Group; items: Conversation[] }[] {
  const pinnedSet = new Set(pinnedIds)
  const pinned: Conversation[] = []
  const rest: Conversation[] = []
  for (const c of items) {
    if (pinnedSet.has(c.id)) pinned.push(c)
    else rest.push(c)
  }

  const map = new Map<Group, Conversation[]>()
  if (pinned.length > 0) {
    map.set('pinned', pinned)
  }
  for (const c of rest) {
    const label = recencyGroup(c.updated_at || c.created_at)
    if (!map.has(label)) map.set(label, [])
    map.get(label)!.push(c)
  }

  return GROUP_ORDER.filter((label) => map.has(label)).map((label) => ({
    label,
    items: map.get(label)!,
  }))
}

type ChatHistoryDrawerProps = {
  open: boolean
  mode: 'overlay' | 'pinned'
  conversations: Conversation[]
  pinnedIds: string[]
  selectedId: string | null
  loading?: boolean
  error?: string | null
  /** The list couldn't be read: shown with Retry instead of "no chats" (#217). */
  loadError?: unknown
  onRetry?: () => void
  retrying?: boolean
  onClose: () => void
  onSelect: (id: string) => void
  onNewChat: () => void
  onRename: (conversation: Conversation) => void
  onTogglePin: (id: string) => void
  onDelete: (conversation: Conversation, event: MouseEvent) => void
  /** Delete the chats chosen in select mode, after one confirmation (#452). */
  onDeleteMany: (conversations: Conversation[]) => void
  onToggleDrawerPinned: () => void
  drawerPinned: boolean
  canPinDrawer: boolean
  renamingId: string | null
  renameValue: string
  onRenameValueChange: (value: string) => void
  onCommitRename: () => void
  onCancelRename: () => void
}

export function ChatHistoryDrawer({
  open,
  mode,
  conversations,
  pinnedIds,
  selectedId,
  loading,
  error,
  loadError,
  onRetry,
  retrying,
  onClose,
  onSelect,
  onNewChat,
  onRename,
  onTogglePin,
  onDelete,
  onDeleteMany,
  onToggleDrawerPinned,
  drawerPinned,
  canPinDrawer,
  renamingId,
  renameValue,
  onRenameValueChange,
  onCommitRename,
  onCancelRename,
}: ChatHistoryDrawerProps) {
  const { t } = useTranslation('chat')
  const [search, setSearch] = useState('')
  const [menuOpenId, setMenuOpenId] = useState<string | null>(null)
  const menuRef = useMenu(menuOpenId, () => setMenuOpenId(null))
  // As an overlay it is a modal dialog: focus moves in and stays, and Escape
  // closes it. Pinned, it is a side panel the page works alongside.
  const overlay = mode === 'overlay'
  // Select mode (#452): chats show checkboxes and the header and footer
  // turn into an action bar. anchor is where a Shift-click range starts.
  const [selecting, setSelecting] = useState(false)
  const [chosen, setChosen] = useState<Set<string>>(() => new Set())
  const [anchor, setAnchor] = useState<string | null>(null)
  const leaveSelect = () => {
    setSelecting(false)
    setChosen(new Set())
    setAnchor(null)
  }
  // Escape leaves select mode first, then closes the drawer.
  const panelRef = useDialog<HTMLElement>(open && overlay, () => (selecting ? leaveSelect() : onClose()))

  const filtered = useMemo(() => {
    // Typos forgiven (#451); exact matches first.
    return searchItems(conversations, search, (c) => [c.title])
  }, [conversations, search])

  const grouped = useMemo(
    () => groupConversations(filtered, pinnedIds),
    [filtered, pinnedIds],
  )
  // The chats in the order shown, for ranges and Select all.
  const shown = useMemo(() => grouped.flatMap((g) => g.items), [grouped])

  // Deleted chats drop out of the selection; when the chosen chats are
  // deleted, select mode ends. A cancelled confirmation keeps it.
  useEffect(() => {
    const ids = new Set(conversations.map((c) => c.id))
    const kept = [...chosen].filter((id) => ids.has(id))
    if (kept.length === chosen.size) return
    setChosen(new Set(kept))
    if (kept.length === 0) setSelecting(false)
    // Only the list changing prunes; choosing doesn't.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [conversations])

  const toggle = (id: string) => {
    setChosen((current) => {
      const next = new Set(current)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
    setAnchor(id)
  }
  const selectRange = (from: string, to: string) => {
    const a = shown.findIndex((c) => c.id === from)
    const b = shown.findIndex((c) => c.id === to)
    if (a < 0 || b < 0) return toggle(to)
    const [lo, hi] = a < b ? [a, b] : [b, a]
    setChosen((current) => new Set([...current, ...shown.slice(lo, hi + 1).map((c) => c.id)]))
  }
  // A click in select mode chooses; outside it, Cmd/Ctrl-click and
  // Shift-click start choosing (Shift from the open chat).
  const clickChat = (id: string, event: MouseEvent) => {
    const range = event.shiftKey
    const one = event.metaKey || event.ctrlKey
    if (!selecting && !range && !one) {
      onSelect(id)
      return
    }
    event.preventDefault()
    setSelecting(true)
    const from = anchor ?? (selecting ? null : selectedId)
    if (range && from) selectRange(from, id)
    else toggle(id)
  }
  const allShown = shown.length > 0 && shown.every((c) => chosen.has(c.id))
  const chosenChats = conversations.filter((c) => chosen.has(c.id))

  useEffect(() => {
    if (!menuOpenId) return
    const close = () => setMenuOpenId(null)
    window.addEventListener('click', close)
    return () => window.removeEventListener('click', close)
  }, [menuOpenId])

  if (!open) return null

  const panel = (
    <aside
      ref={panelRef}
      id="chat-history-drawer"
      className={[
        'chat-history-drawer flex min-h-0 w-[min(20rem,100%)] flex-col bg-surface',
        mode === 'overlay'
          ? 'absolute inset-y-0 start-0 z-30 border-e border-line/40 shadow-panel animate-chat-drawer'
          : 'relative z-10 h-full shrink-0 border-e border-line/50',
      ].join(' ')}
      role={overlay ? 'dialog' : undefined}
      aria-modal={overlay ? true : undefined}
      aria-label={t('history.label')}
      onKeyDown={(e: KeyboardEvent) => {
        // Pinned, there's no dialog to catch Escape.
        if (!overlay && selecting && e.key === 'Escape') {
          e.stopPropagation()
          leaveSelect()
        }
      }}
    >
      {/* The same height and border as the chat's header, so the two line up. */}
      {selecting ? (
        <div className="chat-header ps-4">
          <h2 className="flex-1 font-display text-[15px] font-semibold text-ink" aria-live="polite">
            {t('history.selected', { count: chosen.size })}
          </h2>
          <button type="button" className="chat-header-button text-sm" onClick={leaveSelect}>
            {t('history.cancelSelect')}
          </button>
        </div>
      ) : (
      <div className="chat-header ps-4">
        <h2 className="flex-1 font-display text-[15px] font-semibold text-ink">{t('history.title')}</h2>
        {conversations.length > 0 && (
          <button
            type="button"
            className="chat-header-button text-sm"
            title={t('history.selectHint')}
            onClick={() => setSelecting(true)}
          >
            {t('history.select')}
          </button>
        )}
        {canPinDrawer && (
          <button
            type="button"
            className={['chat-header-button px-0', drawerPinned ? 'bg-primary-soft text-primary-active' : ''].join(' ')}
            title={drawerPinned ? t('history.unpinSidebar') : t('history.pinSidebar')}
            aria-label={drawerPinned ? t('history.unpinSidebar') : t('history.pinSidebar')}
            aria-pressed={drawerPinned}
            onClick={onToggleDrawerPinned}
          >
            {/* A panel docked to the side, filled in while it is. */}
            <svg viewBox="0 0 16 16" className="h-4 w-4" fill="none" stroke="currentColor" strokeWidth={1.4} strokeLinejoin="round" aria-hidden>
              <rect x="2" y="2.5" width="12" height="11" rx="2" />
              <path d="M6.5 2.5v11" />
              {drawerPinned ? <path d="M2.7 3.2h3.8v9.6H2.7z" fill="currentColor" stroke="none" /> : null}
            </svg>
          </button>
        )}
        <button
          type="button"
          className="chat-header-button px-0"
          title={t('history.close')}
          aria-label={t('history.close')}
          onClick={onClose}
        >
          <svg viewBox="0 0 16 16" className="h-4 w-4" fill="none" stroke="currentColor" strokeWidth={1.5} strokeLinecap="round" aria-hidden>
            <path d="m4 4 8 8M12 4l-8 8" />
          </svg>
        </button>
      </div>
      )}

      <div className="shrink-0 px-3 pt-3">
        <label className="relative block">
          <span className="sr-only">{t('history.search')}</span>
          <svg viewBox="0 0 16 16" className="pointer-events-none absolute start-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-ink-faint" fill="none" stroke="currentColor" strokeWidth={1.5} strokeLinecap="round" aria-hidden>
            <circle cx="7" cy="7" r="4.5" />
            <path d="m10.5 10.5 3 3" />
          </svg>
          <input
            type="search"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder={t('history.searchPlaceholder')}
            className="field w-full py-1.5 ps-8 pe-3 text-sm"
            data-autofocus={overlay ? true : undefined}
          />
        </label>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto overflow-x-hidden px-2 py-3">
        {loading && <LoadingSpinner label={t('history.loading')} />}
        {loadError ? (
          <div className="px-2">
            <LoadError compact error={loadError} onRetry={() => onRetry?.()} retrying={retrying} />
          </div>
        ) : null}
        {error && (
          <p className="px-2 text-xs text-danger" role="alert">
            {error}
          </p>
        )}

        {!loading && !loadError && conversations.length === 0 && (
          <div className="px-2 py-6 text-center">
            <p className="text-sm text-ink-muted">{t('history.empty')}</p>
            <button type="button" className="btn-primary mt-4 text-xs" onClick={onNewChat}>
              {t('history.startNew')}
            </button>
          </div>
        )}

        {!loading && conversations.length > 0 && grouped.length === 0 && (
          <p className="px-2 text-xs text-ink-faint">{t('history.noMatches')}</p>
        )}

        {grouped.map((group) => (
          <div key={group.label} className="mb-4">
            <p className="label-caps mb-1 px-2.5">
              {t(`history.groups.${group.label}`)}
            </p>
            <ul className="space-y-0.5">
              {group.items.map((conversation) => {
                const isSelected = selectedId === conversation.id
                const isPinned = pinnedIds.includes(conversation.id)
                const isChosen = chosen.has(conversation.id)
                return (
                  <li key={conversation.id} className="group relative">
                    {renamingId === conversation.id ? (
                      <input
                        className="field w-full py-1.5 text-sm"
                        value={renameValue}
                        autoFocus
                        onChange={(e) => onRenameValueChange(e.target.value)}
                        onBlur={onCommitRename}
                        onKeyDown={(e) => {
                          if (e.key === 'Enter') onCommitRename()
                          if (e.key === 'Escape') onCancelRename()
                        }}
                      />
                    ) : (
                      <>
                        <button
                          type="button"
                          title={conversation.title}
                          role={selecting ? 'checkbox' : undefined}
                          aria-checked={selecting ? isChosen : undefined}
                          onClick={(e) => clickChat(conversation.id, e)}
                          className={[
                            'w-full rounded-lg py-2 ps-2.5 text-start text-sm transition',
                            selecting ? 'pe-2.5' : 'pe-9',
                            selecting && isChosen
                              ? 'bg-primary-soft text-ink'
                              : isSelected && !selecting
                                ? 'bg-primary-soft font-medium text-primary-active'
                                : 'text-ink-muted hover:bg-raised/80 hover:text-ink',
                            isPinned && !isSelected ? 'text-ink' : '',
                          ].join(' ')}
                        >
                          <span className="flex min-w-0 items-center gap-1.5">
                            {selecting ? (
                              <span
                                className={[
                                  'inline-flex h-4 w-4 shrink-0 items-center justify-center rounded border',
                                  isChosen ? 'border-primary bg-primary text-white' : 'border-line bg-surface',
                                ].join(' ')}
                                aria-hidden
                              >
                                {isChosen ? (
                                  <svg viewBox="0 0 16 16" className="h-3 w-3" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
                                    <path d="m3.5 8.5 3 3 6-7" />
                                  </svg>
                                ) : null}
                              </span>
                            ) : null}
                            {isPinned ? (
                              <span
                                className="h-1.5 w-1.5 shrink-0 rounded-full bg-accent"
                                aria-hidden
                                title={t('history.pinned')}
                              />
                            ) : null}
                            <span className="min-w-0 truncate">
                              {conversation.title || t('history.untitled')}
                            </span>
                          </span>
                        </button>
                        <div className={['absolute end-1 top-1/2 -translate-y-1/2', selecting ? 'hidden' : ''].join(' ')}>
                          <button
                            type="button"
                            className={[
                              'inline-flex h-7 w-7 items-center justify-center rounded-md text-ink-faint transition hover:bg-raised hover:text-ink',
                              isSelected || menuOpenId === conversation.id
                                ? 'opacity-100'
                                : 'opacity-0 group-hover:opacity-100 focus:opacity-100',
                            ].join(' ')}
                            aria-label={t('history.actionsFor', { title: conversation.title || t('history.chat') })}
                            aria-expanded={menuOpenId === conversation.id}
                            onClick={(e) => {
                              e.stopPropagation()
                              setMenuOpenId((id) =>
                                id === conversation.id ? null : conversation.id,
                              )
                            }}
                          >
                            <svg viewBox="0 0 16 16" className="h-4 w-4" fill="currentColor" aria-hidden>
                              <circle cx="3.5" cy="8" r="1.2" />
                              <circle cx="8" cy="8" r="1.2" />
                              <circle cx="12.5" cy="8" r="1.2" />
                            </svg>
                          </button>
                          {menuOpenId === conversation.id && (
                            <div
                              className="absolute end-0 top-full z-40 mt-1 min-w-[9rem] rounded-xl border border-line/70 bg-surface p-1 shadow-panel"
                              ref={menuRef}
                              role="menu"
                              onKeyDown={rovingKeyDown}
                              onClick={(e) => e.stopPropagation()}
                            >
                              <button
                                type="button"
                                role="menuitem"
                                className="block w-full rounded-lg px-3 py-1.5 text-start text-sm text-ink hover:bg-raised"
                                onClick={(e) => {
                                  setMenuOpenId(null)
                                  onRename(conversation)
                                  e.stopPropagation()
                                }}
                              >
                                {t('history.rename')}
                              </button>
                              <button
                                type="button"
                                role="menuitem"
                                className="block w-full rounded-lg px-3 py-1.5 text-start text-sm text-ink hover:bg-raised"
                                onClick={(e) => {
                                  setMenuOpenId(null)
                                  onTogglePin(conversation.id)
                                  e.stopPropagation()
                                }}
                              >
                                {isPinned ? t('history.unpin') : t('history.pin')}
                              </button>
                              <button
                                type="button"
                                role="menuitem"
                                className="block w-full rounded-lg px-3 py-1.5 text-start text-sm text-danger hover:bg-danger/10"
                                onClick={(e) => {
                                  setMenuOpenId(null)
                                  onDelete(conversation, e)
                                }}
                              >
                                {t('history.delete')}
                              </button>
                            </div>
                          )}
                        </div>
                      </>
                    )}
                  </li>
                )
              })}
            </ul>
          </div>
        ))}
      </div>
      {selecting ? (
        <div className="flex shrink-0 items-center justify-between gap-2 border-t border-line/50 px-3 py-2.5">
          <button
            type="button"
            className="btn-secondary px-3 py-1.5 text-xs"
            disabled={shown.length === 0}
            onClick={() =>
              setChosen((current) => {
                const next = new Set(current)
                for (const c of shown) {
                  if (allShown) next.delete(c.id)
                  else next.add(c.id)
                }
                return next
              })
            }
          >
            {allShown ? t('history.selectNone') : t('history.selectAll')}
          </button>
          <button
            type="button"
            className="btn-danger px-3 py-1.5 text-xs"
            disabled={chosenChats.length === 0}
            onClick={() => {
              onDeleteMany(chosenChats)
            }}
          >
            {t('history.deleteSelected', { count: chosenChats.length })}
          </button>
        </div>
      ) : null}
    </aside>
  )

  if (mode === 'overlay') {
    return (
      <>
        <button
          type="button"
          className="absolute inset-0 z-20 scrim animate-fade"
          aria-label={t('history.close')}
          onClick={onClose}
        />
        {panel}
      </>
    )
  }

  return panel
}
