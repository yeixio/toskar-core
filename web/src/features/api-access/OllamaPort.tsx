import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ShownError, shownError, type Shown } from '@/components/ui/ShownError'
import { api } from '@/lib/api'
import { Toggle } from './Toggle'

/**
 * Ollama's port (#1396): Ollama's API on port 11434 too, on this computer
 * only, for apps that look for Ollama there and can't be told another
 * address. Off until turned on; Toskar never takes the port from Ollama.
 */
export function OllamaPortCard() {
  const { t } = useTranslation('apiAccess')
  const queryClient = useQueryClient()
  const [error, setError] = useState<Shown | null>(null)
  const settings = useQuery({ queryKey: ['settings'], queryFn: () => api.getSettings(), retry: false })
  const save = useMutation({
    mutationFn: (on: boolean) => api.updateSettings({ ollama_port_enabled: on }),
    onMutate: () => setError(null),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['settings'] }),
    onError: (err) => setError(shownError(err)),
  })
  const data = settings.data
  if (!data) return null
  const on = data.ollama_port_enabled ?? false
  const port = data.api_port ?? 7331
  return (
    <section className="card space-y-3" aria-labelledby="ollama-port">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h2 id="ollama-port" className="section-title">
            {t('ollamaPort.title')}
          </h2>
          <p className="mt-1 text-sm text-ink-muted">{t('ollamaPort.description', { address: `http://localhost:${port}` })}</p>
        </div>
        <Toggle label={t('ollamaPort.label')} checked={on} busy={save.isPending} disabled={save.isPending} onChange={() => save.mutate(!on)} />
      </div>
      {on && data.ollama_port_listening ? <p className="text-sm text-success">{t('ollamaPort.listening')}</p> : null}
      {on && data.ollama_port_error ? (
        <p className="text-sm text-warning">
          {data.ollama_port_error === 'in_use' ? t('ollamaPort.inUse') : t('ollamaPort.failed', { error: data.ollama_port_error })}
        </p>
      ) : null}
      {error ? <ShownError {...error} className="text-sm text-danger" /> : null}
    </section>
  )
}
