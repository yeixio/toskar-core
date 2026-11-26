import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Toggle } from '@/components/ui/Toggle'
import { ShownError, shownError } from '@/components/ui/ShownError'
import { api } from '@/lib/api'
import type { SettingsPatch } from '@/types/api'

/**
 * Community ratings, quality results, and the ask to rate, on the Models
 * page they change (#1391, docs/navigation-and-settings.md section 3.4).
 * They were in Settings › Models & AI.
 */
export function RatingsSettings({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation('settings')
  const queryClient = useQueryClient()
  const settingsQuery = useQuery({ queryKey: ['settings'], queryFn: () => api.getSettings(), retry: false })
  const settings = settingsQuery.data
  const save = useMutation({
    mutationFn: (patch: SettingsPatch) => api.updateSettings(patch),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['settings'] })
      void queryClient.invalidateQueries({ queryKey: ['ratings-community'] })
      void queryClient.invalidateQueries({ queryKey: ['model-rating'] })
      void queryClient.invalidateQueries({ queryKey: ['quality-results'] })
    },
  })
  const busy = save.isPending || !settings
  const rows: { key: 'show' | 'quality' | 'ask'; field: 'community_ratings' | 'quality_results' | 'ratings_prompts'; fallback: boolean }[] = [
    { key: 'show', field: 'community_ratings', fallback: false },
    { key: 'quality', field: 'quality_results', fallback: true },
    { key: 'ask', field: 'ratings_prompts', fallback: true },
  ]
  return (
    <section className="card space-y-4" aria-labelledby="ratings-settings-title">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h2 id="ratings-settings-title" className="section-title">
            {t('ratings.title')}
          </h2>
          <p className="mt-1 text-sm text-ink-muted">{t('ratings.description')}</p>
        </div>
        <button type="button" className="btn-ghost btn-sm" onClick={onClose}>
          {t('ratings.done')}
        </button>
      </div>
      {rows.map((row) => {
        const on = settings?.[row.field] ?? row.fallback
        return (
          <div key={row.key} className="flex items-start justify-between gap-4">
            <div>
              <p className="text-sm font-medium text-ink">{t(`ratings.${row.key}`)}</p>
              <p className="mt-1 text-xs text-ink-muted">{t(`ratings.${row.key}Hint`)}</p>
            </div>
            <Toggle label={t(`ratings.${row.key}`)} checked={on} disabled={busy} onChange={() => save.mutate({ [row.field]: !on })} />
          </div>
        )
      })}
      {save.isError ? <ShownError {...shownError(save.error)} className="text-sm text-danger" /> : null}
    </section>
  )
}
