import i18n, { type Resource } from 'i18next'
import { notifyDesktopLanguage } from '@/lib/desktopBridge'
import { initReactI18next } from 'react-i18next'
import { directionOf, languages, pseudoLocale, sourceLanguage } from './languages'
import { pseudoLocalize } from './pseudo'

// Yggdrasil's UI text comes from the shared catalog in i18n/locales at the
// repository root (multilingual spec §3–7): one folder per language, one JSON
// file per namespace. English is the source; a key missing in a language
// falls back through its base language to English (es-MX → es → en).

const files = import.meta.glob<Record<string, unknown>>('../../../i18n/locales/*/*.json', {
  eager: true,
  import: 'default',
})

/** The catalog as i18next resources: language → namespace → keys. */
export function catalogResources(): Resource {
  const resources: Resource = {}
  for (const [path, keys] of Object.entries(files)) {
    const match = /locales\/([^/]+)\/([^/]+)\.json$/.exec(path)
    if (!match) continue
    const [, language, namespace] = match
    resources[language] = { ...(resources[language] ?? {}), [namespace]: keys }
  }
  return resources
}

const resources = catalogResources()

/** Languages that have a catalog, which the App language can be set to. */
export const availableLanguages = Object.keys(resources)

/** Where the last applied language is kept, so the next start shows it before settings load. */
const storedKey = 'ygg.ui_locale'

function readStored(): string {
  try {
    return localStorage.getItem(storedKey) ?? ''
  } catch {
    return ''
  }
}

/**
 * Picks the language to show. A saved App language wins; otherwise the first
 * of the system's languages that has a catalog, matched exactly or by base
 * language (de-AT → de); otherwise English.
 */
export function resolveLanguage(saved: string, system: readonly string[], available: readonly string[] = availableLanguages): string {
  if (saved === pseudoLocale) return pseudoLocale
  const lower = available.map((a) => a.toLowerCase())
  const match = (tag: string): string | null => {
    const exact = lower.indexOf(tag.toLowerCase())
    if (exact >= 0) return available[exact]
    const base = lower.indexOf(tag.split('-')[0].toLowerCase())
    return base >= 0 ? available[base] : null
  }
  if (saved) {
    const found = match(saved)
    if (found) return found
  }
  for (const tag of system) {
    const found = match(tag)
    if (found) return found
  }
  return sourceLanguage
}

/** The browser's or operating system's preferred languages. */
export function systemLanguages(): string[] {
  if (typeof navigator === 'undefined') return []
  if (navigator.languages?.length) return [...navigator.languages]
  return navigator.language ? [navigator.language] : []
}

/** A tag Intl can format in, or '' for one it can't. */
function canonical(tag: string): string {
  try {
    return Intl.getCanonicalLocales(tag)[0] ?? ''
  } catch {
    return ''
  }
}

// The locale the person asked for: the App language, or the system's first
// language. Text falls back to English while a language has no catalog, but
// Intl formats dates and numbers in any locale, so they follow this.
let requested = canonical(readStored()) || canonical(systemLanguages()[0] ?? '') || sourceLanguage

/** The locale dates, numbers, and sizes are formatted in. */
export function requestedLocale(): string {
  return requested
}

/** Marks the page with its language and direction, for screen readers and RTL layout. */
function markDocument(language: string) {
  if (typeof document === 'undefined') return
  document.documentElement.lang = language
  document.documentElement.dir = directionOf(language)
}

void i18n
  .use(initReactI18next)
  .use({
    type: 'postProcessor',
    name: 'pseudo',
    process: (value: string) => (i18n.language === pseudoLocale ? pseudoLocalize(value) : value),
  })
  .init({
    resources,
    lng: resolveLanguage(readStored(), systemLanguages()),
    // Every language, and the en-XA pseudo-locale, ends at English; i18next
    // tries the base language first (es-MX → es).
    fallbackLng: sourceLanguage,
    supportedLngs: [...availableLanguages, pseudoLocale],
    nonExplicitSupportedLngs: true,
    defaultNS: 'common',
    ns: [...new Set(Object.values(resources).flatMap((r) => Object.keys(r)))],
    postProcess: ['pseudo'],
    interpolation: { escapeValue: false }, // React escapes.
    returnNull: false,
    // Never show a raw key: missing text falls back to English, and the
    // catalog tests catch keys English lacks.
    parseMissingKeyHandler: (key) => {
      if (import.meta.env.DEV) console.warn(`Missing translation: ${key}`)
      const last = key.split(/[.:]/).pop() ?? key
      return last.replace(/([a-z])([A-Z])/g, '$1 $2').replace(/^./, (c) => c.toUpperCase())
    },
    react: { useSuspense: false },
  })

markDocument(i18n.language)
i18n.on('languageChanged', markDocument)
// The desktop shell's menus and tray follow the page's language.
void notifyDesktopLanguage(i18n.language)
i18n.on('languageChanged', (language) => void notifyDesktopLanguage(language))

/** Shows the UI in a language now, and remembers it for the next start. */
export async function applyLanguage(saved: string): Promise<void> {
  const language = resolveLanguage(saved, systemLanguages())
  requested = canonical(saved) || canonical(systemLanguages()[0] ?? '') || sourceLanguage
  try {
    if (saved) localStorage.setItem(storedKey, saved)
    else localStorage.removeItem(storedKey)
  } catch {
    // Storage can be unavailable; the daemon still keeps the setting.
  }
  if (i18n.language !== language) await i18n.changeLanguage(language)
}

export { languages, pseudoLocale, directionOf }
export default i18n
