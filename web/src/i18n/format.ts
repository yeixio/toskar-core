import i18n, { requestedLocale } from './index'
import { pseudoLocale, sourceLanguage } from './languages'

// Locale-aware formatting through Intl (multilingual spec §8). Never build
// dates, numbers, or plurals by hand: German writes 1.234,56 and 30.09.2026.

/**
 * The locale Intl formats in: the one the person asked for, even before its
 * text is translated, so a German system shows 30.09.2026 with English text.
 * The en-XA pseudo-locale formats as English.
 */
export function formatLocale(): string {
  return i18n.language === pseudoLocale ? sourceLanguage : requestedLocale()
}

export function formatNumber(value: number, options?: Intl.NumberFormatOptions): string {
  return new Intl.NumberFormat(formatLocale(), options).format(value)
}

export function formatPercent(fraction: number, maximumFractionDigits = 0): string {
  return formatNumber(fraction, { style: 'percent', maximumFractionDigits })
}

export function formatCurrency(value: number, currency: string): string {
  return formatNumber(value, { style: 'currency', currency })
}

function asDate(value: Date | string | number): Date {
  return value instanceof Date ? value : new Date(value)
}

/** A date such as Sep 30, 2026 (en-US) or 30.09.2026 (de-DE). */
export function formatDate(value: Date | string | number, options: Intl.DateTimeFormatOptions = { dateStyle: 'medium' }): string {
  return new Intl.DateTimeFormat(formatLocale(), options).format(asDate(value))
}

/** A date and time such as Sep 30, 2026, 8:00 PM (en-US) or 30.09.2026, 20:00 (de-DE). */
export function formatDateTime(value: Date | string | number): string {
  return formatDate(value, { dateStyle: 'medium', timeStyle: 'short' })
}

export function formatTime(value: Date | string | number): string {
  return formatDate(value, { timeStyle: 'short' })
}

const relativeSteps: [Intl.RelativeTimeFormatUnit, number][] = [
  ['second', 60],
  ['minute', 60],
  ['hour', 24],
  ['day', 7],
  ['week', 4.34524],
  ['month', 12],
  ['year', Number.POSITIVE_INFINITY],
]

/** Time from now, such as "5 minutes ago" or "in 2 days", in the UI language. */
export function formatRelativeTime(value: Date | string | number, now: Date = new Date()): string {
  let amount = (asDate(value).getTime() - now.getTime()) / 1000
  const formatter = new Intl.RelativeTimeFormat(formatLocale(), { numeric: 'auto' })
  for (const [unit, size] of relativeSteps) {
    if (Math.abs(amount) < size) return formatter.format(Math.round(amount), unit)
    amount /= size
  }
  return formatter.format(Math.round(amount), 'year')
}

/** A size in bytes, such as 1.5 GB or 1,5 GB, with the number in the UI's format. */
export function formatSize(bytes: number): string {
  const units = ['byte', 'kilobyte', 'megabyte', 'gigabyte', 'terabyte'] as const
  let value = bytes
  let unit = 0
  while (Math.abs(value) >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit += 1
  }
  return formatNumber(value, { style: 'unit', unit: units[unit], unitDisplay: 'short', maximumFractionDigits: unit === 0 ? 0 : 1 })
}
