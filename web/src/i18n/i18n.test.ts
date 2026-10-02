import { afterEach, describe, expect, it } from 'vitest'
import i18n, { applyLanguage, resolveLanguage } from './index'
import { formatDate, formatDateTime, formatNumber, formatRelativeTime, formatSize } from './format'
import { directionOf } from './languages'
import { pseudoLocalize } from './pseudo'

afterEach(async () => {
  await applyLanguage('en')
  localStorage.clear()
})

describe('resolveLanguage', () => {
  const available = ['en', 'es', 'de', 'pt-BR', 'zh-Hant']

  it('prefers the saved App language', () => {
    expect(resolveLanguage('de', ['es-MX'], available)).toBe('de')
  })

  it('follows the system languages, by exact tag or base language', () => {
    expect(resolveLanguage('', ['es-MX', 'en'], available)).toBe('es') // es-MX → es
    expect(resolveLanguage('', ['fr-FR', 'de-AT'], available)).toBe('de') // first one with a catalog
    expect(resolveLanguage('', ['pt-BR'], available)).toBe('pt-BR')
    expect(resolveLanguage('', ['zh-Hant'], available)).toBe('zh-Hant')
  })

  it('ends at English', () => {
    expect(resolveLanguage('', ['fr-FR'], available)).toBe('en')
    expect(resolveLanguage('', [], available)).toBe('en')
    expect(resolveLanguage('ja', ['fr'], available)).toBe('en') // a saved language with no catalog yet
  })

  it('keeps the pseudo-locale', () => {
    expect(resolveLanguage('en-XA', ['de'], available)).toBe('en-XA')
  })
})

describe('the pseudo-locale', () => {
  it('accents, lengthens, and brackets text but keeps placeholders and markup', () => {
    const out = pseudoLocalize('Models for {{name}} <b>now</b>')
    expect(out).toMatch(/^\[!! .* !!\]$/)
    expect(out).toContain('{{name}}')
    expect(out).toContain('<b>')
    expect(out).toContain('</b>')
    expect(out).not.toContain('Models')
    expect(out.length).toBeGreaterThan('Models for {{name}} <b>now</b>'.length * 1.2)
  })

  it('applies to every string when chosen', async () => {
    await applyLanguage('en-XA')
    expect(i18n.t('nav.models')).toBe(pseudoLocalize('Models'))
    expect(i18n.t('subsystems.computersConnected', { count: 3 })).toBe(pseudoLocalize('3 computers connected'))
    expect(document.documentElement.lang).toBe('en-XA')
  })
})

describe('the page', () => {
  it('is marked with the language and its direction', async () => {
    await applyLanguage('en')
    expect(document.documentElement.lang).toBe('en')
    expect(document.documentElement.dir).toBe('ltr')
  })

  it('knows right-to-left languages, even without a catalog', () => {
    for (const tag of ['ar', 'he-IL', 'fa', 'ur-PK']) expect(directionOf(tag), tag).toBe('rtl')
    for (const tag of ['en', 'ja', 'de-AT']) expect(directionOf(tag), tag).toBe('ltr')
  })

  it('remembers the App language for the next start, and forgets it for the system default', async () => {
    await applyLanguage('en')
    expect(localStorage.getItem('ygg.ui_locale')).toBe('en')
    await applyLanguage('')
    expect(localStorage.getItem('ygg.ui_locale')).toBeNull()
  })
})

describe('plurals', () => {
  it('use plural rules, not count === 1', () => {
    expect(i18n.t('subsystems.computersConnected', { count: 1 })).toBe('1 computer connected')
    expect(i18n.t('subsystems.computersConnected', { count: 2 })).toBe('2 computers connected')
    expect(i18n.t('subsystems.computersConnected', { count: 0 })).toBe('0 computers connected')
  })
})

// Intl separates a number from its unit with a no-break space in some locales.
const spaced = (text: string) => text.replace(/\s/g, ' ')

describe('formatting', () => {
  const when = new Date(Date.UTC(2026, 8, 30, 20, 0))

  it('follows the locale asked for, even before its text is translated', async () => {
    await applyLanguage('de')
    expect(i18n.language).toBe('en') // no German catalog yet: the text stays English
    expect(document.documentElement.lang).toBe('en')
    expect(formatNumber(1234.56)).toBe('1.234,56')
    expect(spaced(formatSize(1.5 * 1024 ** 3))).toBe('1,5 GB')
    await applyLanguage('en')
    expect(formatNumber(1234.56)).toBe('1,234.56')
    expect(spaced(formatSize(1.5 * 1024 ** 3))).toBe('1.5 GB')
    expect(spaced(formatSize(512))).toBe('512 byte')
  })

  it('formats dates and times', async () => {
    await applyLanguage('de-DE')
    expect(formatDate(when, { dateStyle: 'medium', timeZone: 'UTC' })).toBe('30.09.2026')
    await applyLanguage('en-US')
    expect(formatDate(when, { dateStyle: 'medium', timeZone: 'UTC' })).toBe('Sep 30, 2026')
    expect(formatDateTime(when)).toMatch(/2026/)
  })

  it('formats relative time', async () => {
    await applyLanguage('en')
    const now = new Date(Date.UTC(2026, 8, 30, 20, 0))
    expect(formatRelativeTime(new Date(now.getTime() - 5 * 60_000), now)).toBe('5 minutes ago')
    expect(formatRelativeTime(new Date(now.getTime() + 2 * 86_400_000), now)).toBe('in 2 days')
    expect(formatRelativeTime(new Date(now.getTime() - 86_400_000), now)).toBe('yesterday')
  })

  it('formats the pseudo-locale as English', async () => {
    await applyLanguage('en-XA')
    expect(formatNumber(1234.5)).toBe('1,234.5')
  })
})
