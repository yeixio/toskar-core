// Error codes' text in the App language (multilingual spec §10). It lives
// apart from api.ts, which re-exports it, so components that show errors
// work when a page's test mocks the API.

import i18n from '@/i18n'
import { formatLocale } from '@/i18n/format'

/**
 * The text for an error code in the App language, from errors.json, with the
 * values in details; the service's message, as {{detail}}, for a code the
 * catalog does not have or does not say more about.
 */
export function errorText(code: string | undefined, message: string, details?: Record<string, unknown>): string {
  if (!code || !i18n.exists(`errors:${code}`)) return message
  const values: Record<string, unknown> = { detail: message, ...details }
  // A language comes as a tag, such as ja; it is shown by its name in the App language.
  if (typeof values.language === 'string' && /^[A-Za-z]{2,3}(-[A-Za-z0-9]+)*$/.test(values.language)) {
    try {
      values.language = new Intl.DisplayNames([formatLocale()], { type: 'language' }).of(values.language) ?? values.language
    } catch {
      // Keep the tag.
    }
  }
  return i18n.t(`errors:${code}`, values)
}
