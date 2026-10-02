/** The languages Yggdrasil can show, from the shared catalog in i18n/ at the repository root. */

export type TranslationStatus = 'source' | 'machine' | 'partial' | 'reviewed' | 'community'

export interface Language {
  /** BCP 47 tag, such as en or pt-BR. */
  code: string
  /** The language's name in itself, such as Español. */
  name: string
  dir: 'ltr' | 'rtl'
  status: TranslationStatus
}

const listed = import.meta.glob<Language[]>('../../../i18n/languages.json', { eager: true, import: 'default' })

export const languages: Language[] = Object.values(listed)[0] ?? [{ code: 'en', name: 'English', dir: 'ltr', status: 'source' }]

/** The source language, and the last fallback. */
export const sourceLanguage = 'en'

/** Generated from English to find text that is not translated or does not fit (spec §26). */
export const pseudoLocale = 'en-XA'

// Scripts written right to left. A language not listed in languages.json
// still gets the right direction, so a partial pack never renders backwards.
const rtlLanguages = new Set(['ar', 'arc', 'ckb', 'dv', 'fa', 'ha', 'he', 'iw', 'ks', 'ku', 'ps', 'sd', 'ug', 'ur', 'yi'])

/** The text direction of a language tag. */
export function directionOf(tag: string): 'ltr' | 'rtl' {
  const listedLanguage = languages.find((l) => l.code.toLowerCase() === tag.toLowerCase())
  if (listedLanguage) return listedLanguage.dir
  const base = tag.split('-')[0].toLowerCase()
  return rtlLanguages.has(base) ? 'rtl' : 'ltr'
}
