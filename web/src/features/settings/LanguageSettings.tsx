import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { applyLanguage, availableLanguages, languages, pseudoLocale, pseudoRtlLocale, resolveLanguage, systemLanguages } from '@/i18n'
import { api } from '@/lib/api'
import { useUIStore } from '@/stores/uiStore'
import { AssistantLanguage } from './AssistantLanguage'
import { ReportProblem } from '@/components/ui/ReportProblem'
import { shownError } from '@/components/ui/ShownError'

/** The App language (spec §7): System default, or a language from the catalog. */
export function LanguageSettings() {
  const { t } = useTranslation('settings')
  const queryClient = useQueryClient()
  const advancedMode = useUIStore((s) => s.advancedMode)
  const settings = useQuery({ queryKey: ['settings'], queryFn: () => api.getSettings(), retry: false })
  const saved = settings.data?.ui_locale ?? ''
  const save = useMutation({
    mutationFn: (uiLocale: string) => api.setMyPreferences({ ui_locale: uiLocale }),
    onSuccess: async (_, uiLocale) => {
      await applyLanguage(uiLocale)
      await queryClient.invalidateQueries({ queryKey: ['settings'] })
    },
  })

  const systemLanguage = resolveLanguage('', systemLanguages())
  const nameOf = (code: string) => languages.find((l) => l.code === code)?.name ?? code
  const choices = languages.filter((l) => availableLanguages.includes(l.code))
  const shown = languages.find((l) => l.code === (saved ? resolveLanguage(saved, []) : systemLanguage))

  return (
    <section className="card space-y-4">
      <div>
        <h2 className="section-title">{t('language.title')}</h2>
        <p className="mt-1 text-sm text-ink-muted">{t('language.description')}</p>
      </div>
      <label className="block text-sm">
        <span className="text-ink-muted">{t('language.appLanguage')}</span>
        <select
          className="field mt-1 w-full sm:w-80"
          value={saved}
          disabled={settings.isPending || save.isPending}
          onChange={(e) => save.mutate(e.target.value)}
        >
          <option value="">{t('language.systemDefault', { language: nameOf(systemLanguage) })}</option>
          {choices.map((l) => (
            <option key={l.code} value={l.code} lang={l.code} dir={l.dir}>
              {l.name}
            </option>
          ))}
          {advancedMode || saved === pseudoLocale ? <option value={pseudoLocale}>{t('language.pseudo')}</option> : null}
          {advancedMode || saved === pseudoRtlLocale ? (
            <option value={pseudoRtlLocale} dir="rtl">
              {t('language.pseudoRtl')}
            </option>
          ) : null}
        </select>
      </label>
      {shown && shown.status !== 'source' && shown.status !== 'reviewed' ? (
        <p className="text-xs text-ink-faint">{t(`language.status.${shown.status}`)}</p>
      ) : null}
      {save.isError ? (
        <p className="text-sm text-danger">
          {t('language.saveFailed', { error: save.error instanceof Error ? save.error.message : String(save.error) })}
          <ReportProblem {...shownError(save.error)} className="ms-2 text-xs text-ink-faint underline hover:text-ink" />
        </p>
      ) : null}
      <AssistantLanguage />
    </section>
  )
}
