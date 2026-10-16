import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { formatDate } from '@/i18n/format'
import { api } from '@/lib/api'
import type { TopicActivity, TopicWhere } from '@/types/api'
import { ShownError, shownError, type Shown } from '@/components/ui/ShownError'
import { LoadError } from '@/components/ui/LoadError'

const DAYS = 30

/** The last DAYS days, oldest first, as local yyyy-mm-dd. */
function lastDays(now = new Date()): string[] {
  const out: string[] = []
  for (let i = DAYS - 1; i >= 0; i--) {
    const d = new Date(now.getFullYear(), now.getMonth(), now.getDate() - i)
    out.push(`${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`)
  }
  return out
}

/** Attempts per day: one series, thin bars on a shared baseline. */
function DayStrip({ activity }: { activity: TopicActivity }) {
  const { t } = useTranslation('profiles')
  const counts = new Map(activity.by_day.map((d) => [d.day, d.count]))
  const days = lastDays()
  const max = Math.max(1, ...days.map((d) => counts.get(d) ?? 0))
  return (
    <div
      role="img"
      aria-label={t('topics.attempts.chartLabel', { count: activity.total, days: DAYS })}
      className="flex h-12 items-end gap-[2px] border-b border-line"
    >
      {days.map((day) => {
        const n = counts.get(day) ?? 0
        const label = t('topics.attempts.dayTip', { count: n, date: formatDate(`${day}T12:00:00`, { dateStyle: 'medium' }) })
        return (
          // The hit target is the whole column, taller than the bar.
          <div key={day} className="group flex h-full flex-1 items-end" title={label}>
            <div
              className="w-full rounded-t-[4px] bg-primary/70 group-hover:bg-primary"
              style={{ height: n ? `${Math.max(8, (n / max) * 100)}%` : 0 }}
            />
          </div>
        )
      })}
    </div>
  )
}

/**
 * Off-topic attempts (#345): what this profile held over the last 30 days,
 * where from, and the messages, each of which can be marked as on topic,
 * which adds it to the example questions.
 */
export function TopicsAttempts({ profileId }: { profileId: string }) {
  const { t } = useTranslation('profiles')
  const queryClient = useQueryClient()
  const key = ['topic-attempts', profileId]
  const activity = useQuery({ queryKey: key, queryFn: () => api.getTopicAttempts(profileId, DAYS), retry: false })
  const [error, setError] = useState<Shown | null>(null)
  const mark = useMutation({
    mutationFn: (id: string) => api.markOnTopic(profileId, id),
    onSuccess: () => {
      setError(null)
      void queryClient.invalidateQueries({ queryKey: key })
      void queryClient.invalidateQueries({ queryKey: ['profiles'] })
    },
    onError: (err) => setError(shownError(err)),
  })
  const data = activity.data
  if (!data) {
    return activity.isError ? (
      <LoadError compact error={activity.error} onRetry={() => void activity.refetch()} retrying={activity.isFetching} />
    ) : null
  }
  const whereName = (w: TopicWhere) =>
    w.kind === 'automation' ? t('topics.attempts.where.automation') : t(`topics.attempts.where.${w.kind}`, { name: w.name || w.id || '?' })
  return (
    <section className="max-w-2xl space-y-3 rounded-xl border border-line p-4" aria-labelledby="topics-attempts">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h3 id="topics-attempts" className="label-caps">
          {t('topics.attempts.title')}
        </h3>
        <span className="text-sm text-ink-muted">{t('topics.attempts.total', { count: data.total, days: DAYS })}</span>
      </div>
      {data.total === 0 ? (
        <p className="text-sm text-ink-muted">{t('topics.attempts.none')}</p>
      ) : (
        <>
          <DayStrip activity={data} />
          <ul className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-ink-muted">
            {data.by_where.map((w) => (
              <li key={`${w.kind}:${w.id ?? ''}`}>
                {whereName(w)}: <span className="text-ink">{w.count}</span>
              </li>
            ))}
          </ul>
          <ul className="divide-y divide-line">
            {data.attempts.map((a) => (
              <li key={a.id} className="flex flex-wrap items-start gap-x-3 gap-y-1 py-2 text-sm">
                <div className="min-w-0 flex-1">
                  <p className="break-words text-ink">{a.message}</p>
                  <p className="text-xs text-ink-faint">
                    {formatDate(a.at, { dateStyle: 'medium', timeStyle: 'short' })} · {whereName(a.where)}
                    {a.label === 'answer_off_topic' ? ` · ${t('topics.attempts.replaced')}` : ''}
                  </p>
                </div>
                <button type="button" className="btn-secondary btn-sm" disabled={mark.isPending} onClick={() => mark.mutate(a.id)}>
                  {t('topics.attempts.markOnTopic')}
                </button>
              </li>
            ))}
          </ul>
          {error ? <ShownError {...error} className="text-sm text-danger" /> : null}
        </>
      )}
      <p className="text-xs text-ink-faint">{t('topics.attempts.privacy')}</p>
    </section>
  )
}
