import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { AnywhereSubscription } from '@/lib/api'
import { formatDate } from '@/i18n/format'
import { desktopError, type desktopAnywhere } from '@/lib/desktopBridge'

type Store = NonNullable<ReturnType<typeof desktopAnywhere>>

/** What a refusal from the App Store, the relay, or the app says, by its code. */
const errorKeys: Record<string, string> = {
  ALREADY_COVERED: 'remote.subscription.alreadyCovered',
  SOLD_OUT: 'remote.subscription.soldOut',
  SUBSCRIPTION_IN_USE: 'remote.subscription.inUse',
  MOVE_TOO_SOON: 'remote.subscription.moveTooSoon',
  RENEWAL_NOT_YET: 'remote.subscription.renewalNotYet',
}

/**
 * Toskar's relay in Toskar Pro from the Mac App Store (toskar-apps#245),
 * which sells it with in-app purchase, as guideline 3.1.3(b) asks: the
 * App Store's plans and prices, Restore, and the App Store's own page to
 * manage one bought here. Nothing here points to buying it anywhere else.
 * One bought in the Toskar app on a phone still covers this computer.
 */
export function AppStoreSubscription({ subscription, store }: { subscription: AnywhereSubscription; store: Store }) {
  const { t } = useTranslation('apiAccess')
  const queryClient = useQueryClient()
  const [note, setNote] = useState('')
  const [failure, setFailure] = useState<{ code: string; message: string } | null>(null)
  const active = subscription.state === 'active'
  // Each called with exactly its own arguments: the desktop app refuses a
  // call with more than its method takes, such as React Query's context.
  const plans = useQuery({ queryKey: ['anywhere-plans'], queryFn: () => store.plans(), staleTime: 5 * 60_000, enabled: !active })
  const ownedHere = useQuery({ queryKey: ['anywhere-owned-here'], queryFn: () => store.ownedHere(), enabled: active })

  const settle = (outcome: string) => {
    setFailure(null)
    if (outcome === 'active') {
      setNote('')
      void queryClient.invalidateQueries({ queryKey: ['remote-access'] })
      void queryClient.invalidateQueries({ queryKey: ['anywhere-owned-here'] })
    } else if (outcome === 'pending') {
      setNote(t('remote.subscription.askToBuy'))
    } else if (outcome === 'none') {
      setNote(t('remote.subscription.restoreNone'))
    } else {
      setNote('')
    }
  }
  const fail = (err: unknown) => {
    setNote('')
    setFailure(desktopError(err))
  }
  const buy = useMutation({ mutationFn: (id: string) => store.subscribe(id), onSuccess: settle, onError: fail })
  const restore = useMutation({ mutationFn: (move: boolean) => store.restore(move), onSuccess: settle, onError: fail })
  const manage = useMutation({ mutationFn: () => store.manage(), onError: fail })
  const busy = buy.isPending || restore.isPending

  const failureText = failure
    ? errorKeys[failure.code]
      ? t(errorKeys[failure.code])
      : t('remote.subscription.storeFailed', { message: failure.message || failure.code })
    : ''
  const endedOn = subscription.state === 'ended' && subscription.expires ? formatDate(subscription.expires) : ''
  const soldOut = subscription.open === false
  const onSale = plans.data ?? []

  return (
    <div className="space-y-3 rounded-lg border border-line p-3" aria-labelledby="toskar-relay-subscription">
      <h3 id="toskar-relay-subscription" className="text-sm font-medium">
        {t('remote.subscription.title')}
      </h3>
      {active ? (
        ownedHere.data ? (
          <>
            <p className="text-sm text-success" role="status">
              {t('remote.subscription.activeHere')}
            </p>
            <button type="button" className="btn-secondary" disabled={manage.isPending} onClick={() => manage.mutate()}>
              {t('remote.subscription.manageAppStore')}
            </button>
          </>
        ) : (
          <p className="text-sm text-success" role="status">
            {t(subscription.store === 'app' ? 'remote.subscription.activeApp' : 'remote.subscription.active')}
          </p>
        )
      ) : plans.isSuccess && onSale.length === 0 ? (
        // Not on sale here, as before the App Store has the products.
        <p className="text-sm text-ink-muted">
          {endedOn ? t('remote.subscription.endedInApp', { date: endedOn }) : t('remote.subscription.inAppOnly')}
        </p>
      ) : (
        <>
          <p className="text-sm text-ink-muted">
            {endedOn
              ? t('remote.subscription.endedOn', { date: endedOn })
              : subscription.state === 'ended'
                ? t('remote.subscription.ended')
                : t('remote.subscription.offer')}
          </p>
          {soldOut ? (
            <p className="text-sm text-warning" role="status">
              {t('remote.subscription.soldOut')}
            </p>
          ) : null}
          <div className="flex flex-wrap gap-2">
            {onSale.map((plan, i) => (
              <button
                key={plan.id}
                type="button"
                className={i === 0 ? 'btn-primary' : 'btn-secondary'}
                disabled={busy || soldOut}
                onClick={() => buy.mutate(plan.id)}
              >
                {t(plan.period === 'year' ? 'remote.subscription.yearly' : 'remote.subscription.monthly', { price: plan.price })}
              </button>
            ))}
            <button type="button" className="btn-secondary" disabled={busy} onClick={() => restore.mutate(false)}>
              {restore.isPending ? t('remote.subscription.restoring') : t('remote.subscription.restore')}
            </button>
          </div>
          {buy.isPending ? <p className="text-xs text-ink-muted">{t('remote.subscription.buying')}</p> : null}
          <p className="text-xs text-ink-faint">{t('remote.subscription.appStoreBilling')}</p>
        </>
      )}
      {note ? (
        <p className="text-sm text-ink-muted" role="status">
          {note}
        </p>
      ) : null}
      {failure ? (
        <div className="space-y-2">
          <p className="text-sm text-danger" role="alert">
            {failureText}
          </p>
          {failure.code === 'SUBSCRIPTION_IN_USE' ? (
            <button type="button" className="btn-secondary" disabled={busy} onClick={() => restore.mutate(true)}>
              {t('remote.subscription.moveHere')}
            </button>
          ) : null}
        </div>
      ) : null}
    </div>
  )
}
