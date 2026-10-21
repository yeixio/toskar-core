import { useEffect, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'

import { api } from '@/lib/api'
import { desktopProUnlock } from '@/lib/desktopBridge'

/**
 * Toskar Pro's trial and Unlock (yeixio/toskar-apps#336), above every page:
 * the days left, or that the trial has ended and what still works, with
 * the store's Unlock (and Restore, on the Mac) in the desktop app. Core on
 * its own, and an unlocked Pro, show nothing.
 */
export function ProBanner() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const { data: license } = useQuery({
    queryKey: ['license'],
    queryFn: () => api.getLicense(),
    retry: false,
    refetchInterval: 60 * 60 * 1000,
  })
  const store = desktopProUnlock()
  const [price, setPrice] = useState('')
  const [busy, setBusy] = useState(false)
  const [note, setNote] = useState('')
  const shown = license?.kind === 'trial' || license?.kind === 'ended'

  useEffect(() => {
    if (!shown || !store) return
    let live = true
    void store
      .price()
      .then((found) => {
        if (live) setPrice(found)
      })
      .catch(() => undefined)
    return () => {
      live = false
    }
    // The bridge is the same object for the page's life.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [shown])

  if (!license || !shown) return null

  const run = async (action: () => Promise<string>) => {
    setBusy(true)
    setNote('')
    try {
      const outcome = await action()
      if (outcome === 'unlocked') setNote(t('pro.unlocked'))
      else if (outcome === 'pending') setNote(t('pro.pending'))
      else if (outcome === 'none') setNote(t('pro.noneFound'))
    } catch (error) {
      setNote(t('pro.failed', { detail: error instanceof Error ? error.message : String(error) }))
    } finally {
      setBusy(false)
      void queryClient.invalidateQueries({ queryKey: ['license'] })
    }
  }

  const ended = license.kind === 'ended'
  return (
    <div
      role="status"
      className={`flex flex-wrap items-center gap-x-4 gap-y-2 border-b px-page-x py-2 text-sm ${
        ended ? 'border-warning/40 bg-warning/10 text-ink' : 'border-line/60 bg-primary/10 text-ink'
      }`}
    >
      <p className="min-w-0 flex-1">{ended ? t('pro.ended') : t('pro.trialLeft', { count: license.days_left ?? 0 })}</p>
      {store ? (
        <div className="flex items-center gap-2">
          <button
            type="button"
            disabled={busy}
            onClick={() => void run(store.unlock)}
            className="rounded-lg bg-primary px-3 py-1.5 text-sm font-medium text-primary-fg hover:bg-primary-hover disabled:opacity-60"
          >
            {price ? t('pro.unlockFor', { price }) : t('pro.unlock')}
          </button>
          {license.store === 'mac' && store.restore ? (
            <button
              type="button"
              disabled={busy}
              onClick={() => void run(store.restore!)}
              className="rounded-lg px-3 py-1.5 text-sm text-primary hover:bg-raised/60 disabled:opacity-60"
            >
              {t('pro.restore')}
            </button>
          ) : null}
        </div>
      ) : (
        <p className="text-ink-muted">{t('pro.unlockHere')}</p>
      )}
      {note ? (
        <p className="w-full text-xs text-ink-muted" aria-live="polite">
          {note}
        </p>
      ) : null}
    </div>
  )
}
