import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { api } from '@/lib/api'
import { formatNumber, formatPercent } from '@/i18n/format'
import { modelDisplayName } from '@/features/performance/performanceFormat'

/**
 * How much of a chat's later turns each model on this computer skipped
 * reading again, from its prompt cache, over the last week (#838). A model
 * that skips little re-reads a whole long chat every turn, which is slow.
 */
export function PromptCachePanel() {
  const { t } = useTranslation('diagnostics')
  const query = useQuery({ queryKey: ['prompt-cache'], queryFn: () => api.getPromptCache(), refetchInterval: 60_000 })
  const modelsQuery = useQuery({ queryKey: ['models'], queryFn: () => api.getModels(), retry: false })
  // An older daemon, or a week with no later turns, has nothing to show.
  const models = Array.isArray(query.data?.models) ? query.data.models : []
  if (models.length === 0) return null
  return (
    <section className="card space-y-3" aria-labelledby="prompt-cache-title">
      <div>
        <h2 id="prompt-cache-title" className="section-title">
          {t('promptCache.title')}
        </h2>
        <p className="mt-1 text-sm text-ink-muted">{t('promptCache.description')}</p>
      </div>
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead className="text-ink-muted">
            <tr>
              <th scope="col" className="py-1 pe-4 text-start font-normal">{t('promptCache.model')}</th>
              <th scope="col" className="py-1 pe-4 text-start font-normal">{t('promptCache.reused')}</th>
            </tr>
          </thead>
          <tbody>
            {models.map((m) => (
              <tr key={m.model_id} className="border-t border-line/50 align-top">
                <th scope="row" className="py-2 pe-4 text-start font-medium text-ink">
                  {modelDisplayName(modelsQuery.data ?? [], m.model_id)}
                </th>
                <td className="py-2 pe-4">
                  <span className={`tabular-nums ${m.low ? 'text-warning' : 'text-ink'}`}>{formatPercent(m.reuse)}</span>
                  <span className="block text-xs text-ink-muted">
                    {t('promptCache.detail', { count: m.turns, cached: formatNumber(m.cached_tokens), prompt: formatNumber(m.prompt_tokens) })}
                  </span>
                  {m.low && <span className="block text-xs text-warning">{t('promptCache.low')}</span>}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  )
}
