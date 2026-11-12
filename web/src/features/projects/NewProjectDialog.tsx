import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '@/lib/api'
import { useDialog } from '@/lib/useDialog'
import { ShownError, shownError } from '@/components/ui/ShownError'
import type { Project } from '@/types/api'

/** Names a new project (#1136); the rest is set on its page. */
export function NewProjectDialog({ onClose, onCreated }: { onClose: () => void; onCreated: (project: Project) => void }) {
  const { t } = useTranslation('projects')
  const queryClient = useQueryClient()
  const ref = useDialog<HTMLFormElement>(true, onClose)
  const [name, setName] = useState('')
  const create = useMutation({
    mutationFn: () => api.createProject({ name: name.trim() }),
    onSuccess: (project) => {
      void queryClient.invalidateQueries({ queryKey: ['projects'] })
      if (project) onCreated(project)
    },
  })
  return (
    <div className="fixed inset-0 z-50 flex items-end justify-center scrim p-4 sm:items-center" role="presentation" onClick={onClose}>
      <form
        ref={ref}
        className="card w-full max-w-sm space-y-4 shadow-panel"
        role="dialog"
        aria-modal="true"
        aria-labelledby="new-project-title"
        onClick={(event) => event.stopPropagation()}
        onSubmit={(event) => {
          event.preventDefault()
          if (name.trim()) create.mutate()
        }}
      >
        <div>
          <h2 id="new-project-title" className="font-display text-lg font-semibold text-ink">
            {t('new.title')}
          </h2>
          <p className="mt-1 text-sm text-ink-muted">{t('new.body')}</p>
        </div>
        <label className="block space-y-1">
          <span className="text-sm text-ink">{t('new.name')}</span>
          <input
            className="field w-full"
            value={name}
            maxLength={200}
            data-autofocus
            placeholder={t('new.placeholder')}
            onChange={(event) => setName(event.target.value)}
          />
        </label>
        {create.error ? <ShownError {...shownError(create.error)} className="text-sm text-danger" /> : null}
        <div className="flex flex-wrap justify-end gap-2">
          <button type="button" className="btn-secondary" onClick={onClose}>
            {t('new.cancel')}
          </button>
          <button type="submit" className="btn-primary" disabled={!name.trim() || create.isPending}>
            {t('new.create')}
          </button>
        </div>
      </form>
    </div>
  )
}
