import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { fitLabelText } from '@/features/models/modelPresentation'
import { formatBytes } from '@/lib/format'
import type { Model } from '@/types/api'
import { taskModelSuggestion } from './taskModel'

/** Says which model does work like this well, under the chat's folder. */
export function TaskModelHint({ models }: { models: Model[] }) {
  const { t } = useTranslation('chat')
  const suggestion = taskModelSuggestion(models)
  if (!suggestion) return null
  const { model, runsHere } = suggestion
  const name = model.display_name || model.id
  return (
    <p className="mb-2 px-1 text-xs text-ink-muted">
      {runsHere
        ? t('task.betterModel', { name, size: formatBytes(model.size_bytes), fit: fitLabelText(model.fit?.label) })
        : t('task.betterModelTooLarge', { name })}{' '}
      {runsHere ? (
        <Link to="/models" className="font-medium text-primary underline-offset-2 hover:underline">
          {t('task.installOnModels')}
        </Link>
      ) : null}
    </p>
  )
}
