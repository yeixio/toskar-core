import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { directionOf } from '@/i18n'
import { answerLanguages, nameInItself } from '@/i18n/answerLanguages'
import { api } from '@/lib/api'
import type { AssistantLanguageMode } from '@/types/api'
import { ReportProblem } from '@/components/ui/ReportProblem'
import { shownError } from '@/components/ui/ShownError'
import { LoadError } from '@/components/ui/LoadError'

/**
 * The assistant language (multilingual spec §11, §30): the language answers
 * are written in, apart from the App language. A language asked for in a
 * message, such as "answer in English", always wins.
 */
export function AssistantLanguage() {
  const { t } = useTranslation('settings')
  const queryClient = useQueryClient()
  const settings = useQuery({ queryKey: ['settings'], queryFn: () => api.getSettings(), retry: false })
  const mode: AssistantLanguageMode = settings.data?.assistant_language_mode ?? 'auto'
  const language = settings.data?.assistant_language ?? ''
  const value = mode === 'language' && language ? language : mode === 'app' ? ':app' : ':auto'
  const save = useMutation({
    mutationFn: (choice: string) =>
      api.setMyPreferences(
        choice === ':auto'
          ? { assistant_language_mode: 'auto' }
          : choice === ':app'
            ? { assistant_language_mode: 'app' }
            : { assistant_language_mode: 'language', assistant_language: choice },
      ),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['settings'] }),
  })
  const choices = answerLanguages.includes(language) || !language ? answerLanguages : [...answerLanguages, language]

  return (
    <div className="space-y-1">
      {settings.isError ? (
        <LoadError compact error={settings.error} onRetry={() => void settings.refetch()} retrying={settings.isFetching} />
      ) : null}
      <label className="block text-sm">
        <span className="text-ink-muted">{t('language.assistantLanguage')}</span>
        <select
          className="field mt-1 w-full sm:w-80"
          value={value}
          disabled={!settings.data || save.isPending}
          onChange={(e) => save.mutate(e.target.value)}
        >
          <option value=":auto">{t('language.assistantAuto')}</option>
          <option value=":app">{t('language.assistantApp')}</option>
          {choices.map((tag) => (
            <option key={tag} value={tag} lang={tag} dir={directionOf(tag)}>
              {nameInItself(tag)}
            </option>
          ))}
        </select>
      </label>
      <p className="text-xs text-ink-faint">{t('language.assistantHint')}</p>
      {save.isError ? (
        <p className="text-sm text-danger">
          {t('language.saveFailed', { error: save.error instanceof Error ? save.error.message : String(save.error) })}
          <ReportProblem {...shownError(save.error)} className="ms-2 text-xs text-ink-faint underline hover:text-ink" />
        </p>
      ) : null}
    </div>
  )
}
