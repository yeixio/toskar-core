import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type AnywhereSubscription } from '@/lib/api'
import { formatCurrency } from '@/i18n/format'
import { isDesktopShell, openExternal } from '@/lib/desktopBridge'
import { ShownError, shownError, type Shown } from '@/components/ui/ShownError'

/** What access from anywhere costs, as the stores and Stripe charge it, in US dollars. */
const anywherePrices = { monthly: 4.99, yearly: 39.99 } as const

/**
 * Opens a checkout or billing page: in the person's browser from the
 * desktop app, else in the tab opened during the click, so the browser
 * doesn't block it, or a new one.
 */
function openPage(url: string, tab: Window | null) {
  if (isDesktopShell()) {
    tab?.close()
    openExternal(url)
    return
  }
  if (tab && !tab.closed) {
    tab.location.href = url
    return
  }
  window.open(url, '_blank', 'noopener')
}

/** A blank tab opened during the click, for the page once it's known; none in the desktop app. */
function blankTab(): Window | null {
  if (isDesktopShell()) return null
  const tab = window.open('', '_blank')
  if (tab) tab.opener = null
  return tab
}

/**
 * Toskar's relay, bought here (#639): Subscribe while there's none, a note
 * while the checkout is open (it turns on by itself once paid), and Manage
 * for one bought here. One bought in the Toskar app is managed in the
 * device's store.
 */
export function ToskarSubscription({ subscription }: { subscription: AnywhereSubscription }) {
  const { t } = useTranslation('apiAccess')
  const queryClient = useQueryClient()
  const [error, setError] = useState<Shown | null>(null)
  const checkout = useMutation({
    mutationFn: async ({ plan, tab }: { plan: 'monthly' | 'yearly'; tab: Window | null }) => {
      try {
        const out = await api.startSubscriptionCheckout(plan)
        if (!out?.url) throw new Error(t('remote.subscription.noPage'))
        openPage(out.url, tab)
      } catch (err) {
        tab?.close()
        throw err
      }
    },
    onSuccess: () => {
      setError(null)
      void queryClient.invalidateQueries({ queryKey: ['remote-access'] })
    },
    onError: (err) => setError(shownError(err)),
  })
  const manage = useMutation({
    mutationFn: async (tab: Window | null) => {
      try {
        const out = await api.manageSubscription()
        if (!out?.url) throw new Error(t('remote.subscription.noPage'))
        openPage(out.url, tab)
      } catch (err) {
        tab?.close()
        throw err
      }
    },
    onSuccess: () => setError(null),
    onError: (err) => setError(shownError(err)),
  })
  const busy = checkout.isPending || manage.isPending
  // The App Store's copy only points to the Toskar app (App Review 3.1.1).
  if (subscription.in_app_only && subscription.state !== 'active') {
    return (
      <div className="space-y-2 rounded-lg border border-line p-3" aria-labelledby="toskar-relay-subscription">
        <h3 id="toskar-relay-subscription" className="text-sm font-medium">
          {t('remote.subscription.title')}
        </h3>
        <p className="text-sm text-ink-muted">{t('remote.subscription.inAppOnly')}</p>
      </div>
    )
  }
  const soldOut = subscription.open === false
  const subscribe = (plan: 'monthly' | 'yearly') => checkout.mutate({ plan, tab: blankTab() })

  return (
    <div className="space-y-3 rounded-lg border border-line p-3" aria-labelledby="toskar-relay-subscription">
      <h3 id="toskar-relay-subscription" className="text-sm font-medium">
        {t('remote.subscription.title')}
      </h3>
      {subscription.state === 'active' ? (
        <>
          <p className="text-sm text-success" role="status">
            {t(subscription.store === 'app' ? 'remote.subscription.activeApp' : 'remote.subscription.active')}
          </p>
          {subscription.store === 'stripe' && !subscription.in_app_only ? (
            <button type="button" className="btn-secondary" disabled={busy} onClick={() => manage.mutate(blankTab())}>
              {manage.isPending ? t('remote.subscription.opening') : t('remote.subscription.manage')}
            </button>
          ) : null}
        </>
      ) : subscription.state === 'pending' ? (
        <>
          <p className="text-sm text-ink-muted" role="status">
            {t('remote.subscription.pending')}
          </p>
          {subscription.checkout ? (
            <a className="text-sm underline" href={subscription.checkout} target="_blank" rel="noopener noreferrer">
              {t('remote.subscription.reopen')}
            </a>
          ) : null}
        </>
      ) : (
        <>
          <p className="text-sm text-ink-muted">
            {subscription.state === 'ended' ? t('remote.subscription.ended') : t('remote.subscription.offer')}
          </p>
          {soldOut ? (
            <p className="text-sm text-warning" role="status">
              {t('remote.subscription.soldOut')}
            </p>
          ) : null}
          <div className="flex flex-wrap gap-2">
            <button type="button" className="btn-primary" disabled={busy || soldOut} onClick={() => subscribe('monthly')}>
              {t('remote.subscription.monthly', { price: formatCurrency(anywherePrices.monthly, 'USD') })}
            </button>
            <button type="button" className="btn-secondary" disabled={busy || soldOut} onClick={() => subscribe('yearly')}>
              {t('remote.subscription.yearly', { price: formatCurrency(anywherePrices.yearly, 'USD') })}
            </button>
          </div>
          {checkout.isPending ? <p className="text-xs text-ink-muted">{t('remote.subscription.opening')}</p> : null}
          <p className="text-xs text-ink-faint">{t('remote.subscription.billing')}</p>
        </>
      )}
      {subscription.error ? <p className="text-xs text-warning">{t('remote.subscription.error', { code: subscription.error })}</p> : null}
      {error ? <ShownError {...error} className="text-sm text-danger" /> : null}
    </div>
  )
}
