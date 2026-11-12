import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '@/lib/api'
import { useDialog } from '@/lib/useDialog'
import { ShownError, shownError } from '@/components/ui/ShownError'
import { LoadError } from '@/components/ui/LoadError'
import type { Conversation } from '@/types/api'

/**
 * Moves a chat into one of the person's projects (#1136), or into a new
 * one named here. A chat is in one project at most, so moving it takes it
 * out of the one it was in.
 */
export function MoveToProjectDialog({ conversation, onClose }: { conversation: Conversation; onClose: () => void }) {
  const { t } = useTranslation('chat')
  const queryClient = useQueryClient()
  const ref = useDialog(true, onClose)
  const projects = useQuery({ queryKey: ['projects'], queryFn: () => api.listProjects(), retry: false })
  const [name, setName] = useState('')
  const move = useMutation({
    mutationFn: async (target: { id: string } | { name: string }) => {
      let id = 'id' in target ? target.id : ''
      if ('name' in target) {
        const made = await api.createProject({ name: target.name })
        if (!made) throw new Error(t('moveDialog.failed'))
        id = made.id
      }
      await api.updateConversation(conversation.id, { project_id: id })
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['conversations'] })
      void queryClient.invalidateQueries({ queryKey: ['projects'] })
      onClose()
    },
  })
  const list = (projects.data ?? []).filter((p) => p.id !== conversation.project_id)
  return (
    <div className="fixed inset-0 z-50 flex items-end justify-center scrim p-4 sm:items-center" role="presentation" onClick={onClose}>
      <div
        ref={ref}
        className="card flex max-h-[85vh] w-full max-w-sm flex-col gap-4 shadow-panel"
        role="dialog"
        aria-modal="true"
        aria-labelledby="move-project-title"
        onClick={(event) => event.stopPropagation()}
      >
        <div>
          <h2 id="move-project-title" className="font-display text-lg font-semibold text-ink">
            {t('moveDialog.title')}
          </h2>
          <p className="mt-1 break-words text-sm text-ink-muted">
            {t('moveDialog.body', { title: conversation.title || t('history.untitled') })}
          </p>
        </div>
        {projects.isError ? (
          <LoadError compact error={projects.error} onRetry={() => void projects.refetch()} retrying={projects.isFetching} />
        ) : projects.isSuccess && list.length === 0 ? (
          <p className="text-sm text-ink-muted">{t('moveDialog.none')}</p>
        ) : (
          <ul className="min-h-0 space-y-1 overflow-y-auto">
            {list.map((project) => (
              <li key={project.id}>
                <button
                  type="button"
                  className="w-full rounded-lg px-3 py-2 text-start text-sm text-ink hover:bg-raised disabled:opacity-50"
                  disabled={move.isPending}
                  onClick={() => move.mutate({ id: project.id })}
                >
                  {project.name}
                </button>
              </li>
            ))}
          </ul>
        )}
        <form
          className="flex gap-2"
          onSubmit={(event) => {
            event.preventDefault()
            if (name.trim()) move.mutate({ name: name.trim() })
          }}
        >
          <input
            className="field min-w-0 flex-1 text-sm"
            value={name}
            maxLength={200}
            aria-label={t('moveDialog.newLabel')}
            placeholder={t('moveDialog.newPlaceholder')}
            onChange={(event) => setName(event.target.value)}
          />
          <button type="submit" className="btn-secondary px-3 py-1 text-xs" disabled={!name.trim() || move.isPending}>
            {t('moveDialog.create')}
          </button>
        </form>
        {move.error ? <ShownError {...shownError(move.error)} className="text-sm text-danger" /> : null}
        <div className="flex justify-end">
          <button type="button" className="btn-secondary" data-autofocus onClick={onClose}>
            {t('moveDialog.cancel')}
          </button>
        </div>
      </div>
    </div>
  )
}
