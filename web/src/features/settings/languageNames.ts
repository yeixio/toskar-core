import { formatLocale } from '@/i18n/format'

export function languageName(tag: string): string {
  try {
    return new Intl.DisplayNames([formatLocale()], { type: 'language' }).of(tag) ?? tag
  } catch {
    return tag
  }
}

/** Language tags in the order of their names as shown, not their codes (#1342). */
export function byLanguageName(tags: string[]): string[] {
  const collator = new Intl.Collator(formatLocale())
  return [...tags].sort((a, b) => collator.compare(languageName(a), languageName(b)))
}
