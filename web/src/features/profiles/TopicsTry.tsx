import { useMutation } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '@/lib/api'
import type { TopicPolicy, TopicTrial } from '@/types/api'
import { ShownError, shownOr, type Shown } from '@/components/ui/ShownError'

// Each is profiles:topics.try.samples.<key>: made-up attempts to talk the
// assistant off topic, and small talk it should still answer.
const SAMPLES = ['poem', 'ignore', 'boss', 'roleplay', 'hello', 'whatCanYouDo'] as const

const LABEL_STYLE: Record<TopicTrial['label'], string> = {
  on_topic: 'bg-success/15 text-success',
  small_talk: 'bg-primary/15 text-primary',
  off_topic: 'bg-warning/15 text-warning',
  '': 'bg-ink/10 text-ink-muted',
}

interface Tried {
  message: string
  result: TopicTrial
  enforce: boolean
}

/**
 * Try it (#345): type a question, or pick a made-up attempt to talk the
 * assistant off topic, and see the label and the reply, with the topic
 * controls as they are in the editor, before saving.
 */
export function TopicsTry({ profileId, topics, examples }: { profileId: string; topics?: TopicPolicy; examples: string[] }) {
  const { t } = useTranslation('profiles')
  const [message, setMessage] = useState('')
  const [tried, setTried] = useState<Tried[]>([])
  const [error, setError] = useState<Shown | null>(null)
  const run = useMutation({
    mutationFn: (m: string) => api.tryTopics(profileId, m, topics),
    onSuccess: (result, m) => {
      setError(null)
      if (result) setTried((list) => [{ message: m, result, enforce: topics?.strictness === 'enforce' }, ...list].slice(0, 10))
    },
    onError: (err) => setError(shownOr(err, t('topics.try.failed'))),
  })
  if (!topics) return null
  const ask = (m: string) => {
    const text = m.trim()
    if (text && !run.isPending) run.mutate(text)
  }
  const samples = [...examples.slice(0, 1), ...SAMPLES.map((k) => t(`topics.try.samples.${k}`))]
  return (
    <section className="max-w-2xl space-y-3 rounded-xl border border-line p-4" aria-labelledby="topics-try">
      <h3 id="topics-try" className="label-caps">
        {t('topics.try.title')}
      </h3>
      <p className="text-xs text-ink-muted">{t('topics.try.intro')}</p>
      <div className="flex flex-wrap gap-1.5">
        {samples.map((s) => (
          <button key={s} type="button" className="btn-secondary btn-sm" disabled={run.isPending} onClick={() => ask(s)}>
            {s}
          </button>
        ))}
      </div>
      <form
        className="flex gap-2"
        onSubmit={(e) => {
          e.preventDefault()
          ask(message)
          setMessage('')
        }}
      >
        <input
          className="field min-w-0 flex-1"
          maxLength={2000}
          aria-label={t('topics.try.message')}
          placeholder={t('topics.try.placeholder')}
          value={message}
          onChange={(e) => setMessage(e.target.value)}
        />
        <button type="submit" className="btn-primary" disabled={!message.trim() || run.isPending}>
          {run.isPending ? t('topics.try.trying') : t('topics.try.send')}
        </button>
      </form>
      {error ? <ShownError {...error} className="text-sm text-danger" /> : null}
      <ul className="space-y-2" aria-live="polite">
        {tried.map(({ message: m, result, enforce }, i) => (
          <li key={tried.length - i} className="space-y-1 rounded-lg bg-ink/5 p-3 text-sm">
            <p className="text-ink">{m}</p>
            <p className="flex flex-wrap items-center gap-2 text-xs">
              <span className={`status-chip ${LABEL_STYLE[result.label]}`}>{t(`topics.try.label.${result.label || 'unchecked'}`)}</span>
              {result.held ? <span className="text-ink-muted">{t('topics.try.held')}</span> : null}
              {result.replaced ? <span className="text-ink-muted">{t('topics.try.replaced')}</span> : null}
              {!enforce && !result.held && result.label === 'off_topic' ? <span className="text-ink-muted">{t('topics.try.guideNote')}</span> : null}
            </p>
            <p className="whitespace-pre-wrap text-ink-muted">{result.reply}</p>
          </li>
        ))}
      </ul>
    </section>
  )
}
