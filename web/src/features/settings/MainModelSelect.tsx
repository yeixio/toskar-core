import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { api } from '@/lib/api'
import { formatSize } from '@/i18n/format'
import { canChat } from '@/features/models/modelPresentation'
import type { MainModelView } from '@/types/api'

/**
 * Picks the main chat model (#1481): Auto, or a chat model installed on this
 * computer. It sets the Fast model of the profile new chats use, so the
 * choice needs no Advanced mode. Settings › Models & AI and the chat card
 * both show it.
 */
export function MainModelSelect({
  main,
  disabled,
  onChange,
  id,
  autoFocus,
}: {
  main: MainModelView | undefined
  disabled?: boolean
  onChange: (modelId: string) => void
  id?: string
  autoFocus?: boolean
}) {
  const { t } = useTranslation('settings')
  const modelsQuery = useQuery({ queryKey: ['models'], queryFn: () => api.getModels(), retry: false })
  const models = (modelsQuery.data ?? []).filter((m) => m.installed && canChat(m))
  const value = main?.chosen ? main.model_id : ''
  return (
    <select
      id={id}
      className="field w-full"
      aria-label={t('lifecycle.mainModelPick')}
      value={value}
      disabled={disabled || modelsQuery.isLoading}
      autoFocus={autoFocus}
      onChange={(e) => onChange(e.target.value)}
    >
      <option value="">{main && !main.chosen ? t('lifecycle.mainModelAutoNamed', { model: main.name }) : t('lifecycle.mainModelAuto')}</option>
      {models.map((m) => (
        <option key={m.id} value={m.id}>
          {m.memory_needed_bytes
            ? t('lifecycle.mainModelOption', { model: m.display_name || m.id, size: formatSize(m.memory_needed_bytes) })
            : m.display_name || m.id}
        </option>
      ))}
      {/* Kept when the list couldn't be read, so the choice still shows (#217). */}
      {value && !models.some((m) => m.id === value) ? <option value={value}>{main?.name ?? value}</option> : null}
    </select>
  )
}
