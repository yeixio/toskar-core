import { requestWords } from './index'
import type { RequestWords } from './types'

// Turns the phrases in a language's words into regular expressions. The
// phrase marks are described in types.ts.

/** A request as phrases are matched against it: NFKC, lowercase, plain apostrophes, single spaces. */
export function normalizeRequest(text: string): string {
  return text.normalize('NFKC').toLowerCase().replace(/[’‘]/g, "'").replace(/\s+/g, ' ').trim()
}

/** Reads numerals written in characters before a counter, so 八点 becomes 8点. */
export function withDigits(text: string, words: RequestWords): string {
  const { tens, counters } = words
  if (!tens || !counters?.length) return text
  const chars = Object.keys(words.numbers).filter((key) => [...key].length === 1)
  const run = new RegExp(`[${chars.map(escapeClass).join('')}]+(?=\\s*(?:${alternatives(counters)}))`, 'gu')
  return text.replace(run, (numeral) => {
    const value = readNumeral(numeral, words.numbers, tens)
    return value == null ? numeral : String(value)
  })
}

function readNumeral(numeral: string, numbers: Record<string, number>, tens: string): number | null {
  if (numbers[numeral] != null && numeral !== tens) return numbers[numeral]
  const at = numeral.indexOf(tens)
  if (at < 0) return null
  const before = numeral.slice(0, at)
  const after = numeral.slice(at + tens.length)
  const ten = before ? numbers[before] : 1
  const one = after ? numbers[after] : 0
  if (ten == null || one == null) return null
  return ten * 10 + one
}

const cache = new WeakMap<readonly string[], Map<string, RegExp | null>>()

/** One regular expression that finds any of the phrases, or null when there are none. */
export function phrases(list: readonly string[], spaced: boolean, flags = ''): RegExp | null {
  let byFlags = cache.get(list)
  if (!byFlags) {
    byFlags = new Map()
    cache.set(list, byFlags)
  }
  const key = `${spaced}:${flags}`
  if (!byFlags.has(key)) {
    byFlags.set(key, list.length ? new RegExp(phraseSource(list, spaced), `u${flags}`) : null)
  }
  return byFlags.get(key) ?? null
}

/** Whether any of the phrases is in the text. */
export function hasPhrase(text: string, list: readonly string[], spaced: boolean): boolean {
  return phrases(list, spaced)?.test(text) ?? false
}

/** The phrases as one non-capturing group, longest first, or a group that never matches. */
export function phraseSource(list: readonly string[], spaced: boolean, edges: { start?: boolean; end?: boolean } = {}): string {
  if (!list.length) return '(?!)'
  const sorted = [...list].sort((a, b) => b.length - a.length)
  return `(?:${sorted.map((phrase) => compile(phrase, spaced, edges)).join('|')})`
}

function compile(phrase: string, spaced: boolean, edges: { start?: boolean; end?: boolean }): string {
  let out = ''
  for (const ch of phrase) {
    if (ch === '(') out += '(?:'
    else if (ch === ')' || ch === '|' || ch === '?') out += ch
    else if (ch === '…') out += '.{0,40}?'
    else if (ch === '#') out += amountSource()
    else if (ch === ' ') out += spaced ? '\\s+' : '\\s*'
    else out += escape(ch)
  }
  if (!spaced) return out
  // Whole words only: "unter" is not the start of "unterhalb".
  const start = edges.start !== false && /^\(*[\p{L}\p{N}]/u.test(phrase) ? '(?<![\\p{L}\\p{N}])' : ''
  const end = edges.end !== false && /[\p{L}\p{N}).?]$/u.test(phrase) ? '(?![\\p{L}\\p{N}])' : ''
  return `${start}(?:${out})${end}`
}

/** The words as plain alternatives, for counters and other single tokens. */
function alternatives(list: readonly string[]): string {
  return [...list].sort((a, b) => b.length - a.length).map(escape).join('|')
}

function escape(text: string): string {
  return text.replace(/[.*+?^${}()|[\]\\/]/g, '\\$&')
}

function escapeClass(ch: string): string {
  return /[\]\\^-]/.test(ch) ? `\\${ch}` : ch
}

// Amounts: $500, 500 €, R$ 2.500, 1 299,99 €, 5万円, 50만 원.

const NUMBER = String.raw`\d{1,3}(?:[.,\u00a0\u202f' ]\d{3})+(?:[.,]\d{1,2})?|\d+(?:[.,]\d+)?`

let amountPattern = ''

function allLanguages(): RequestWords[] {
  return Object.values(requestWords)
}

/** Currency symbols and words of every language, so "500 €" reads in any of them. */
function symbolSource(): string {
  const symbols = new Set(allLanguages().flatMap((words) => Object.keys(words.currencies)))
  return tokens([...symbols])
}

function multiplierSource(): string {
  const multipliers = new Set(allLanguages().flatMap((words) => Object.keys(words.multipliers)))
  return tokens([...multipliers])
}

// A Latin-letter token such as "eur" or "k" must not run into a word ("europe", "km").
function tokens(list: string[]): string {
  if (!list.length) return '(?!)'
  const sorted = [...list].sort((a, b) => b.length - a.length)
  return `(?:${sorted
    .map((token) => {
      const start = /^\p{Script=Latin}/u.test(token) ? '(?<!\\p{Script=Latin})' : ''
      const end = /\p{Script=Latin}$/u.test(token) ? '(?!\\p{Script=Latin})' : ''
      return `${start}${escape(token)}${end}`
    })
    .join('|')})`
}

/** An amount with an optional currency before or after it, without capturing groups. */
export function amountSource(): string {
  if (!amountPattern) {
    const symbol = symbolSource()
    amountPattern = `(?:${symbol}\\s*)?(?:${NUMBER})(?:\\s*${multiplierSource()})?(?:\\s*${symbol})?`
  }
  return amountPattern
}

let amountReader: RegExp | null = null

export interface Amount {
  value: number
  /** The currency the amount names, or undefined when it names none. */
  currency?: string
}

/** Reads an amount that amountSource matched. Symbols resolve in the given languages first (¥ is yuan in Chinese). */
export function readAmount(text: string, order: readonly RequestWords[]): Amount | null {
  if (!amountReader) {
    const symbol = symbolSource()
    amountReader = new RegExp(`^(?:(${symbol})\\s*)?(${NUMBER})(?:\\s*(${multiplierSource()}))?(?:\\s*(${symbol}))?`, 'u')
  }
  const match = amountReader.exec(text.trim())
  if (!match) return null
  const value = readNumber(match[2])
  if (value == null) return null
  const multiplier = match[3] ? lookup(match[3], order, (words) => words.multipliers) ?? 1 : 1
  const symbol = match[1] ?? match[4]
  const currency = symbol ? lookup(symbol, order, (words) => words.currencies) : undefined
  return { value: round(value * multiplier), currency }
}

function lookup<T>(key: string, order: readonly RequestWords[], table: (words: RequestWords) => Record<string, T>): T | undefined {
  for (const words of [...order, ...allLanguages()]) {
    const found = table(words)[key]
    if (found !== undefined) return found
  }
  return undefined
}

/**
 * A number as people write it: 1,299.99 and 1.299,99 are the same, and a
 * single separator before exactly three digits groups thousands (2.500 is
 * 2500), while one before one or two digits is the decimal point (4.99).
 */
export function readNumber(text: string): number | null {
  if (!/^\d[\d.,\u00a0\u202f' ]*$/.test(text)) return null
  const separators = [...text.matchAll(/[^\d]/g)]
  if (!separators.length) return Number(text)
  const last = separators[separators.length - 1]
  const lastIndex = last.index ?? 0
  const digitsAfter = text.length - lastIndex - 1
  const kinds = new Set(separators.map((s) => s[0]))
  const decimal = /[.,]/.test(last[0]) && (digitsAfter !== 3 || (kinds.size > 1 && separators.length > 1 && last[0] !== separators[0][0]))
  const whole = (decimal ? text.slice(0, lastIndex) : text).replace(/[^\d]/g, '')
  const fraction = decimal ? text.slice(lastIndex + 1) : ''
  const value = Number(fraction ? `${whole}.${fraction}` : whole)
  return Number.isFinite(value) ? value : null
}

function round(value: number): number {
  return Math.round(value * 100) / 100
}
