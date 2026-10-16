import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { api } from '@/lib/api'
import type { PersonalStyle } from '@/types/api'
import { ShownError, shownOr, type Shown } from '@/components/ui/ShownError'
import { LoadError } from '@/components/ui/LoadError'

const KEY = ['personalization'] as const

// Each choice's label and options are settings:personalization.<key> in the catalog.
const SELECTS: { key: 'length' | 'tone' | 'format' | 'units'; options: string[] }[] = [
  { key: 'length', options: ['brief', 'balanced', 'detailed'] },
  { key: 'tone', options: ['friendly', 'neutral', 'direct'] },
  { key: 'format', options: ['prose', 'lists'] },
  { key: 'units', options: ['metric', 'imperial'] },
]

/**
 * How answers look (spec §38). These shape style only: what tools may do is
 * set under Tool permissions, and a preference here can never change it.
 */
export function Personalization() {
  const { t } = useTranslation('settings')
  const queryClient = useQueryClient()
  const query = useQuery({ queryKey: KEY, queryFn: () => api.getPersonalStyle(), retry: false })
  const [draft, setDraft] = useState<PersonalStyle>({})
  const [error, setError] = useState<Shown | null>(null)
  const [saved, setSaved] = useState(false)

  useEffect(() => {
    if (query.data) setDraft(query.data)
  }, [query.data])

  const save = useMutation({
    mutationFn: () => api.setPersonalStyle(draft),
    onSuccess: (data) => {
      setError(null)
      setSaved(true)
      if (data) queryClient.setQueryData(KEY, data)
    },
    onError: (err) => {
      setSaved(false)
      setError(shownOr(err, t('personalization.saveFailed')))
    },
  })

  const set = (patch: PersonalStyle) => {
    setSaved(false)
    setDraft((d) => ({ ...d, ...patch }))
  }

  return (
    <section className="card space-y-4">
      <div>
        <h2 className="section-title">{t('personalization.title')}</h2>
        <p className="mt-1 text-sm text-ink-muted">{t('personalization.description')}</p>
      </div>
      {query.isError ? (
        <LoadError compact error={query.error} onRetry={() => void query.refetch()} retrying={query.isFetching} />
      ) : null}
      <div className="grid gap-3 sm:grid-cols-2">
        {SELECTS.map((s) => (
          <label key={s.key} className="block text-sm">
            <span className="text-ink-muted">{t(`personalization.${s.key}.label`)}</span>
            <select
              className="field mt-1 w-full"
              value={draft[s.key] ?? ''}
              onChange={(e) => set({ [s.key]: e.target.value } as PersonalStyle)}
            >
              <option value="">{t('personalization.noPreference')}</option>
              {s.options.map((value) => (
                <option key={value} value={value}>
                  {t(`personalization.${s.key}.${value}`)}
                </option>
              ))}
            </select>
          </label>
        ))}
      </div>
      <label className="block text-sm">
        <span className="text-ink-muted">{t('personalization.aboutYou')}</span>
        <textarea
          className="field mt-1 min-h-20 w-full"
          maxLength={1500}
          placeholder={t('personalization.aboutYouPlaceholder')}
          value={draft.about_me ?? ''}
          onChange={(e) => set({ about_me: e.target.value })}
        />
      </label>
      <label className="block text-sm">
        <span className="text-ink-muted">{t('personalization.instructions')}</span>
        <textarea
          className="field mt-1 min-h-20 w-full"
          maxLength={1500}
          placeholder={t('personalization.instructionsPlaceholder')}
          value={draft.instructions ?? ''}
          onChange={(e) => set({ instructions: e.target.value })}
        />
      </label>
      <div className="flex items-center gap-3">
        <button type="button" className="btn-primary px-3 py-1.5 text-xs" disabled={save.isPending || !query.data} onClick={() => save.mutate()}>
          {save.isPending ? t('personalization.saving') : t('personalization.save')}
        </button>
        {saved && <span className="text-xs text-ink-faint">{t('personalization.saved')}</span>}
      </div>
      {error && <ShownError {...error} className="text-xs text-danger" />}
    </section>
  )
}
