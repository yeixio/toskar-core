import { de } from './de'
import { en } from './en'
import { es } from './es'
import { fr } from './fr'
import { it } from './it'
import { ja } from './ja'
import { ko } from './ko'
import { ptBR } from './pt-BR'
import type { RequestWords } from './types'
import { zhHans, zhHant } from './zh'

export type { Daypart, RequestWords } from './types'

/** The languages automation requests can be written in, by catalog code. */
export const requestWords: Record<string, RequestWords> = {
  en,
  de,
  es,
  fr,
  it,
  'pt-BR': ptBR,
  ja,
  ko,
  'zh-Hans': zhHans,
  'zh-Hant': zhHant,
}

/** The words for a language tag: an exact match, then one with the same base language (pt → pt-BR). */
export function requestWordsFor(language: string): RequestWords | undefined {
  const tag = language.toLowerCase()
  const exact = Object.keys(requestWords).find((code) => code.toLowerCase() === tag)
  if (exact) return requestWords[exact]
  const base = tag.split('-')[0]
  const sameBase = Object.keys(requestWords).find((code) => code.toLowerCase().split('-')[0] === base)
  return sameBase ? requestWords[sameBase] : undefined
}

/**
 * The words to read a request with, in order: the App language's, then
 * English, which everyone can write in.
 */
export function requestLanguages(language: string): RequestWords[] {
  const own = requestWordsFor(language)
  return own && own !== en ? [own, en] : [en]
}
