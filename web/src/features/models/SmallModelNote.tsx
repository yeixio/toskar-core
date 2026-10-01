import type { Model } from '@/types/api'
import { SMALL_MODEL_NOTE, isSmallModel } from './modelPresentation'

/**
 * For a small model: a plain note that it can get details wrong, and a larger
 * model that fits this computer. Larger models get nothing, so the note stays
 * meaningful.
 */
export function SmallModelNote({
  model,
  alternative,
  onInstallAlternative,
}: {
  model: Model
  alternative?: Model | null
  onInstallAlternative?: (id: string) => void
}) {
  if (!isSmallModel(model)) return null
  const other = alternative && alternative.id !== model.id ? alternative : null
  return (
    <div role="note" className="mt-2 rounded-lg bg-warning/10 px-2.5 py-2 text-xs leading-relaxed text-ink">
      <p>{SMALL_MODEL_NOTE}</p>
      {other ? (
        other.installed ? (
          <p className="mt-1 text-ink-muted">
            {other.display_name} is installed. Pick it, or Auto, in chat for those questions.
          </p>
        ) : onInstallAlternative ? (
          <p className="mt-1 text-ink-muted">
            {other.display_name} fits this computer.{' '}
            <button
              type="button"
              className="font-medium text-primary underline-offset-2 hover:underline"
              onClick={() => onInstallAlternative(other.id)}
            >
              Install {other.display_name}
            </button>
          </p>
        ) : null
      ) : null}
    </div>
  )
}
