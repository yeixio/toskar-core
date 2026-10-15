import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '@/lib/api'
import { useDialog } from '@/lib/useDialog'
import type { ConversationSummary } from '@/types/api'
import { ShownError, shownError, type Shown } from '@/components/ui/ShownError'

/**
 * A chat's auto-compact summary (#342): what the model is told about the
 * earlier conversation, which the person reads and corrects. A correction
 * is kept by later summaries.
 */
export function SummaryDialog({ summary, onClose }: { summary: ConversationSummary; onClose: () => void }) {
  const { t } = useTranslation('chat')
  const queryClient = useQueryClient()
  const ref = useDialog(true, onClose)
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState(summary.summary)
  const [error, setError] = useState<Shown | null>(null)
  const save = useMutation({
    mutationFn: () => api.editConversationSummary(summary.conversation_id, draft),
    onSuccess: (next) => {
      queryClient.setQueryData(['summary', summary.conversation_id], next)
      setEditing(false)
      setError(null)
    },
    onError: (err) => setError(shownError(err)),
  })
  return (
    <div className="fixed inset-0 z-50 flex items-end justify-center scrim p-4 sm:items-center" role="presentation" onClick={onClose}>
      <div
        ref={ref}
        className="card flex max-h-[85vh] w-full max-w-xl flex-col gap-3 shadow-panel"
        role="dialog"
        aria-modal="true"
        aria-labelledby="summary-title"
        onClick={(event) => event.stopPropagation()}
      >
        <div>
          <h2 id="summary-title" className="font-display text-lg font-semibold text-ink">
            {t('compact.summaryTitle')}
          </h2>
          <p className="mt-1 text-sm text-ink-muted">{t('compact.summaryBody', { count: summary.message_count })}</p>
        </div>
        {editing ? (
          <textarea
            className="field min-h-48 w-full flex-1 text-sm leading-relaxed"
            value={draft}
            aria-label={t('compact.summaryTitle')}
            data-autofocus
            onChange={(e) => setDraft(e.target.value)}
          />
        ) : (
          <div className="min-h-0 flex-1 overflow-y-auto whitespace-pre-wrap rounded-lg bg-raised p-3 text-sm leading-relaxed text-ink">
            {summary.summary}
          </div>
        )}
        {summary.edited && !editing ? <p className="text-xs text-ink-faint">{t('compact.edited')}</p> : null}
        {editing ? <p className="text-xs text-ink-faint">{t('compact.editHint')}</p> : null}
        {error ? <ShownError {...error} className="text-sm text-danger" /> : null}
        <div className="flex flex-wrap justify-end gap-2">
          {editing ? (
            <>
              <button
                type="button"
                className="btn-secondary"
                onClick={() => {
                  setDraft(summary.summary)
                  setEditing(false)
                }}
              >
                {t('compact.cancel')}
              </button>
              <button type="button" className="btn-primary" disabled={save.isPending || !draft.trim()} onClick={() => save.mutate()}>
                {t('compact.save')}
              </button>
            </>
          ) : (
            <>
              <button type="button" className="btn-secondary" onClick={() => setEditing(true)}>
                {t('compact.edit')}
              </button>
              <button type="button" className="btn-primary" data-autofocus onClick={onClose}>
                {t('compact.close')}
              </button>
            </>
          )}
        </div>
      </div>
    </div>
  )
}
