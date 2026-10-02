import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { applyLanguage, availableLanguages, languages, pseudoLocale, resolveLanguage, systemLanguages } from '@/i18n'
import { api } from '@/lib/api'
import { useUIStore } from '@/stores/uiStore'

/** The App language (spec §7): System default, or a language from the catalog. */
export function LanguageSettings() {
  const { t } = useTranslation('settings')
  const queryClient = useQueryClient()
  const advancedMode = useUIStore((s) => s.advancedMode)
  const settings = useQuery({ queryKey: ['settings'], queryFn: () => api.getSettings(), retry: false })
  const saved = settings.data?.ui_locale ?? ''
  const save = useMutation({
    mutationFn: (uiLocale: string) => api.updateSettings({ ui_locale: uiLocale }),
    onSuccess: async (_, uiLocale) => {
      await applyLanguage(uiLocale)
      await queryClient.invalidateQueries({ queryKey: ['settings'] })
    },
  })

  const systemLanguage = resolveLanguage('', systemLanguages())
  const nameOf = (code: string) => languages.find((l) => l.code === code)?.name ?? code
  const choices = languages.filter((l) => availableLanguages.includes(l.code))

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
        </select>
      </label>
      {save.isError ? (
        <p className="text-sm text-danger">
          {t('language.saveFailed', { error: save.error instanceof Error ? save.error.message : String(save.error) })}
        </p>
      ) : null}
    </section>
  )
}
