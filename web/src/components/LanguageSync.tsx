import { useQuery } from '@tanstack/react-query'
import { useEffect } from 'react'
import { applyLanguage } from '@/i18n'
import { api } from '@/lib/api'

/**
 * Shows the UI in the App language the daemon keeps (spec §6–7), so the
 * desktop app, the browser, and the iPhone app agree. With no App language
 * set, it follows the system's languages, including when they change.
 */
export function LanguageSync() {
  const settings = useQuery({ queryKey: ['settings'], queryFn: () => api.getSettings(), retry: false, staleTime: 30_000 })
  const saved = settings.data?.ui_locale

  useEffect(() => {
    if (saved === undefined) return
    void applyLanguage(saved)
    if (saved) return
    const onChange = () => void applyLanguage('')
    window.addEventListener('languagechange', onChange)
    return () => window.removeEventListener('languagechange', onChange)
  }, [saved])

  return null
}
