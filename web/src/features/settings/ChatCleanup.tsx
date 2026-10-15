import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '@/lib/api'
import { useUIStore } from '@/stores/uiStore'
import type { Conversation } from '@/types/api'
import { olderChats } from './olderChats'
import { ShownError, shownOr } from '@/components/ui/ShownError'

const AGES = [30, 90, 365] as const
// The most chats one bulk delete takes (#452).
const BATCH = 1000

async function deleteAll(ids: string[]) {
  for (let i = 0; i < ids.length; i += BATCH) {
    await api.deleteConversations(ids.slice(i, i + BATCH))
  }
}

/**
 * Deleting chats from Settings (#452): those older than a set age, with
 * the count before you confirm, or all of them. Pinned chats are kept by
 * the age delete. Confirmed in the page, since window.confirm is
 * unreliable in the desktop app's WebView.
 */
export function ChatCleanup() {
  const { t } = useTranslation('settings')
  const queryClient = useQueryClient()
  const pinned = useUIStore((s) => s.pinnedConversationIds)
  const [days, setDays] = useState<number>(90)
  const [confirming, setConfirming] = useState<'older' | 'all' | null>(null)
  const [done, setDone] = useState<string | null>(null)
  const chats = useQuery({ queryKey: ['conversations'], queryFn: () => api.getConversations(), retry: false })
  const all = useMemo(() => chats.data ?? [], [chats.data])
  const older = useMemo(() => olderChats(all, days, pinned), [all, days, pinned])

  const remove = useMutation({
    mutationFn: (ids: string[]) => deleteAll(ids),
    onSuccess: (_data, ids) => {
      setDone(t('history.deleted', { count: ids.length }))
      setConfirming(null)
      queryClient.invalidateQueries({ queryKey: ['conversations'] })
      queryClient.invalidateQueries({ queryKey: ['performance'] })
    },
    onError: () => setConfirming(null),
  })
  const error = remove.isError ? shownOr(remove.error, t('history.deleteFailed')) : null

  const confirmRow = (text: string, chosen: Conversation[]) => (
    <div role="alertdialog" aria-label={text} className="space-y-2 rounded-lg border border-danger/40 bg-danger/5 p-3">
      <p className="text-sm text-ink">{text}</p>
      <p className="text-xs text-ink-muted">{t('history.confirmDetail')}</p>
      <div className="flex flex-wrap justify-end gap-2">
        <button type="button" className="btn-secondary px-3 py-1.5 text-xs" disabled={remove.isPending} onClick={() => setConfirming(null)}>
          {t('history.cancel')}
        </button>
        <button type="button" className="btn-danger px-3 py-1.5 text-xs" disabled={remove.isPending} onClick={() => remove.mutate(chosen.map((c) => c.id))}>
          {remove.isPending ? t('history.clearing') : t('history.deleteCount', { count: chosen.length })}
        </button>
      </div>
    </div>
  )

  return (
    <div className="space-y-3 border-t border-line/50 pt-4">
      <div className="flex flex-wrap items-center gap-2">
        <label className="flex items-center gap-2 text-sm font-medium text-ink">
          {t('history.olderThan')}
          <select
            className="field py-1 text-sm"
            value={days}
            onChange={(e) => {
              setDays(Number(e.target.value))
              setConfirming(null)
              setDone(null)
            }}
          >
            {AGES.map((d) => (
              <option key={d} value={d}>
                {t('history.days', { count: d })}
              </option>
            ))}
          </select>
        </label>
        <button
          type="button"
          className="btn-secondary px-3 py-1.5 text-xs"
          disabled={older.length === 0 || remove.isPending || chats.isLoading}
          onClick={() => {
            setDone(null)
            setConfirming('older')
          }}
        >
          {older.length === 0 ? t('history.noneOlder') : t('history.deleteCount', { count: older.length })}
        </button>
      </div>
      {confirming === 'older' ? confirmRow(t('history.confirmOlder', { count: older.length, days }), older) : null}
      {pinned.length > 0 ? <p className="text-xs text-ink-faint">{t('history.pinnedKept')}</p> : null}
      <button
        type="button"
        className="btn-secondary px-3 py-1.5 text-xs"
        disabled={all.length === 0 || remove.isPending}
        onClick={() => {
          setDone(null)
          setConfirming('all')
        }}
      >
        {t('history.clear')}
      </button>
      {confirming === 'all' ? confirmRow(t('history.confirmAll', { count: all.length }), all) : null}
      {done ? (
        <p role="status" className="text-xs text-ink-muted">
          {done}
        </p>
      ) : null}
      {error ? <ShownError {...error} role="alert" className="text-xs text-danger" /> : null}
    </div>
  )
}
