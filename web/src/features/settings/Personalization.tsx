import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import type { PersonalStyle } from '@/types/api'

const KEY = ['personalization'] as const

const SELECTS: { key: 'length' | 'tone' | 'format' | 'units'; label: string; options: [string, string][] }[] = [
  { key: 'length', label: 'Answer length', options: [['brief', 'Brief'], ['balanced', 'Balanced'], ['detailed', 'Detailed']] },
  { key: 'tone', label: 'Tone', options: [['friendly', 'Friendly'], ['neutral', 'Neutral'], ['direct', 'Direct']] },
  { key: 'format', label: 'Format', options: [['prose', 'Paragraphs'], ['lists', 'Lists']] },
  { key: 'units', label: 'Units', options: [['metric', 'Metric'], ['imperial', 'Imperial']] },
]

/**
 * How answers look (spec §38). These shape style only: what tools may do is
 * set under Tool permissions, and a preference here can never change it.
 */
export function Personalization() {
  const queryClient = useQueryClient()
  const query = useQuery({ queryKey: KEY, queryFn: () => api.getPersonalStyle(), retry: false })
  const [draft, setDraft] = useState<PersonalStyle>({})
  const [error, setError] = useState('')
  const [saved, setSaved] = useState(false)

  useEffect(() => {
    if (query.data) setDraft(query.data)
  }, [query.data])

  const save = useMutation({
    mutationFn: () => api.setPersonalStyle(draft),
    onSuccess: (data) => {
      setError('')
      setSaved(true)
      if (data) queryClient.setQueryData(KEY, data)
    },
    onError: (err) => {
      setSaved(false)
      setError(err instanceof Error ? err.message : 'Could not save.')
    },
  })

  const set = (patch: PersonalStyle) => {
    setSaved(false)
    setDraft((d) => ({ ...d, ...patch }))
  }

  return (
    <section className="card space-y-4">
      <div>
        <h2 className="section-title">Personalization</h2>
        <p className="mt-1 text-sm text-ink-muted">
          How answers should look, in every chat and automation. This shapes style only. What the AI may do is set under
          Tool permissions, and nothing here or in Memory can change it.
        </p>
      </div>
      <div className="grid gap-3 sm:grid-cols-2">
        {SELECTS.map((s) => (
          <label key={s.key} className="block text-sm">
            <span className="text-ink-muted">{s.label}</span>
            <select
              className="field mt-1 w-full"
              value={draft[s.key] ?? ''}
              onChange={(e) => set({ [s.key]: e.target.value } as PersonalStyle)}
            >
              <option value="">No preference</option>
              {s.options.map(([value, label]) => (
                <option key={value} value={value}>
                  {label}
                </option>
              ))}
            </select>
          </label>
        ))}
      </div>
      <label className="block text-sm">
        <span className="text-ink-muted">About you</span>
        <textarea
          className="field mt-1 min-h-20 w-full"
          maxLength={1500}
          placeholder="I'm a backend engineer in Juneau. My main project is a Go service."
          value={draft.about_me ?? ''}
          onChange={(e) => set({ about_me: e.target.value })}
        />
      </label>
      <label className="block text-sm">
        <span className="text-ink-muted">Anything else about how to answer</span>
        <textarea
          className="field mt-1 min-h-20 w-full"
          maxLength={1500}
          placeholder="Show code examples in Go. Mention sources at the end."
          value={draft.instructions ?? ''}
          onChange={(e) => set({ instructions: e.target.value })}
        />
      </label>
      <div className="flex items-center gap-3">
        <button type="button" className="btn-primary px-3 py-1.5 text-xs" disabled={save.isPending} onClick={() => save.mutate()}>
          {save.isPending ? 'Saving…' : 'Save'}
        </button>
        {saved && <span className="text-xs text-ink-faint">Saved</span>}
      </div>
      {error && <p className="text-xs text-danger">{error}</p>}
    </section>
  )
}
