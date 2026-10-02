import i18n from '@/i18n'
import { formatDate, formatPrice } from '@/i18n/format'
import type { AutomationCondition, AutomationNotification, AutomationSchedule } from '@/types/api'
import { type Daypart, type RequestWords, requestLanguages } from './requestWords'
import { amountSource, hasPhrase, normalizeRequest, phrases, phraseSource, readAmount, withDigits } from './requestWords/match'

// The parser reads a request in the App language or in English ("every
// morning at 8", "jeden Morgen um 8 Uhr", "毎朝8時に") with the words in
// requestWords/. What it shows the person, such as schedules, notes, and
// errors, is in the App language.

export interface ParsedAutomation {
  name: string
  prompt: string
  schedule: AutomationSchedule
  notification: AutomationNotification
  notes: string[]
}

/** A weekday's name in the App language: 0 is Sunday. */
export function weekdayName(index: number): string {
  return i18n.t(`automations:weekdays.${index}`)
}

// The run reports the price in the threshold's currency, so the daemon can
// compare the numbers as they are.
function priceInstruction(currency: string | undefined): string {
  const unit = currency ? ` in ${currency}` : ''
  return `Include a JSON object in the result with the numeric price${unit}, for example {"price": 420}.`
}
const PRICE_INSTRUCTION_LINE = /Include a JSON object in the result with the numeric price(?: in [A-Z]{3})?, for example \{"price": 420\}\./g
const AVAILABLE_INSTRUCTION =
  'Include a JSON object in the result, {"available": true} when the item is available and {"available": false} when it is not.'
const SIGNIFICANT_INSTRUCTION =
  'Include a JSON object in the result, {"significant": true} when this is worth a notification and {"significant": false} when it is not.'

export function localTimeZone(): string {
  return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'
}

export function parseAutomationRequest(
  text: string,
  now: Date,
  timeZone: string,
  language: string = i18n.resolvedLanguage ?? i18n.language,
): ParsedAutomation {
  const original = text.trim()
  if (!original) {
    throw new Error(i18n.t('automations:parse.describe'))
  }
  if (!timeZone) {
    throw new Error(i18n.t('automations:parse.timeZone'))
  }
  const normalized = normalizeRequest(original)
  // The App language's words first, then English's. The first that finds
  // a schedule is the language the request is written in.
  const languages = requestLanguages(language)
  for (const words of languages) {
    const schedule = parseSchedule(withDigits(normalized, words), words, now, timeZone)
    if (!schedule) continue
    const order = [words, ...languages.filter((other) => other !== words)]
    // An amount without a currency is in the App language's.
    const notification = parseNotification(normalized, order, languages[0].currency)
    return {
      name: automationName(normalized, original, notification, order),
      prompt: withSignalInstruction(readableTask(original, notification, words), notification),
      schedule: schedule.schedule,
      notification,
      notes: schedule.notes,
    }
  }
  throw new Error(i18n.t('automations:parse.describeWhen'))
}

export function scheduleLabel(schedule: AutomationSchedule): string {
  switch (schedule.kind) {
    case 'once':
      return schedule.at
        ? i18n.t('automations:schedule.onceAt', { when: formatWhen(schedule.at, schedule.time_zone) })
        : i18n.t('automations:schedule.once')
    case 'daily':
      return i18n.t('automations:schedule.daily', { time: clockLabel(schedule.hour ?? 0, schedule.minute ?? 0) })
    case 'weekly':
      return i18n.t('automations:schedule.weekly', {
        day: weekdayName(schedule.weekday ?? 0),
        time: clockLabel(schedule.hour ?? 0, schedule.minute ?? 0),
      })
    case 'interval':
      return intervalLabel(schedule.every_seconds ?? 0)
    default:
      return i18n.t('automations:schedule.scheduled')
  }
}

export function notificationLabel(notification: AutomationNotification): string {
  switch (notification.mode) {
    case 'none':
      return i18n.t('automations:notify.none')
    case 'change':
      return i18n.t('automations:notify.change')
    case 'condition':
      return conditionLabel(notification.condition)
    default:
      return i18n.t('automations:notify.always')
  }
}

export function formatWhen(iso: string | undefined, timeZone: string): string {
  if (!iso) return i18n.t('automations:time.notScheduled')
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return i18n.t('automations:time.notScheduled')
  return formatDate(date, { timeZone: timeZone || 'UTC', dateStyle: 'medium', timeStyle: 'short' })
}

export function civilInputValue(iso: string | undefined, timeZone: string): string {
  if (!iso) return ''
  const parts = zonedParts(new Date(iso), timeZone)
  const month = String(parts.month).padStart(2, '0')
  const day = String(parts.day).padStart(2, '0')
  const hour = String(parts.hour).padStart(2, '0')
  const minute = String(parts.minute).padStart(2, '0')
  return `${parts.year}-${month}-${day}T${hour}:${minute}`
}

export function civilToISO(value: string, timeZone: string): string {
  const match = value.match(/^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})/)
  if (!match) {
    throw new Error(i18n.t('automations:parse.chooseDateTime'))
  }
  return instantInZone(
    Number(match[1]),
    Number(match[2]),
    Number(match[3]),
    Number(match[4]),
    Number(match[5]),
    timeZone,
  ).toISOString()
}

interface ScheduleFound {
  schedule: AutomationSchedule
  notes: string[]
}

function parseSchedule(text: string, words: RequestWords, now: Date, timeZone: string): ScheduleFound | null {
  const notes: string[] = []
  const interval = parseInterval(text, words)
  if (interval) {
    return { schedule: { kind: 'interval', time_zone: timeZone, every_seconds: interval }, notes }
  }
  const weekday = parseWeekday(text, words)
  if (weekday != null) {
    const clock = parseClock(text, words)
    const hour = clock?.hour ?? 8
    const minute = clock?.minute ?? 0
    if (!clock) notes.push(i18n.t('automations:parse.noTime8'))
    return {
      schedule: { kind: 'weekly', time_zone: timeZone, hour, minute, weekday },
      notes,
    }
  }
  const daily = dailyPart(text, words)
  if (daily) {
    const clock = parseClock(text, words)
    let hour = clock?.hour
    let minute = clock?.minute
    if (!clock) {
      const named = namedDaypart(daily === 'day' ? daypartIn(text, words) ?? 'morning' : daily)
      hour = named.hour
      minute = named.minute
      notes.push(named.note)
    }
    return {
      schedule: { kind: 'daily', time_zone: timeZone, hour: hour ?? 8, minute: minute ?? 0 },
      notes,
    }
  }
  const once = onceDay(text, words)
  if (once) {
    const clock = parseClock(text, words)
    const hour = clock?.hour ?? 9
    const minute = clock?.minute ?? 0
    if (!clock) notes.push(i18n.t('automations:parse.noTime9'))
    const today = zonedParts(now, timeZone)
    const day = once === 'today' ? today : addDays(today, 1)
    const at = instantInZone(day.year, day.month, day.day, hour, minute, timeZone)
    return { schedule: { kind: 'once', time_zone: timeZone, at: at.toISOString() }, notes }
  }
  return null
}

// Regular expressions built from a language's words, made once per language.
const built = new WeakMap<RequestWords, Map<string, RegExp | null>>()

function pattern(words: RequestWords, name: string, source: () => string | null, flags = 'u'): RegExp | null {
  let patterns = built.get(words)
  if (!patterns) {
    patterns = new Map()
    built.set(words, patterns)
  }
  if (!patterns.has(name)) {
    const text = source()
    patterns.set(name, text == null ? null : new RegExp(text, flags))
  }
  return patterns.get(name) ?? null
}

function gap(words: RequestWords): string {
  return words.spaced ? '\\s+' : '\\s*'
}

function parseInterval(text: string, words: RequestWords): number | null {
  if (hasPhrase(text, words.interval.halfHour, words.spaced)) return 30 * 60
  if (hasPhrase(text, words.interval.hourly, words.spaced)) return 60 * 60
  const { units, before, after } = words.interval
  const amount = `(?<q>\\d+|${phraseSource(Object.keys(words.numbers), words.spaced)})`
  const unit = `(?:(?<second>${phraseSource(units.second, words.spaced)})|(?<minute>${phraseSource(units.minute, words.spaced)})|(?<hour>${phraseSource(units.hour, words.spaced)})|(?<day>${phraseSource(units.day, words.spaced)}))`
  const forms = [
    pattern(words, 'intervalBefore', () => (before.length ? `${phraseSource(before, words.spaced)}(?:${gap(words)}${amount})?${gap(words)}${unit}` : null), 'gu'),
    pattern(words, 'intervalAfter', () => (after.length ? `(?:(?<![\\d])${amount}${gap(words)})?${unit}\\s*${phraseSource(after, words.spaced, { start: false })}` : null), 'gu'),
  ]
  for (const form of forms) {
    if (!form) continue
    for (const match of text.matchAll(form)) {
      const groups = match.groups ?? {}
      const count = groups.q ? quantity(groups.q, words) : 1
      if (count == null || count <= 0) continue
      const seconds = groups.second ? 1 : groups.minute ? 60 : groups.hour ? 3600 : 86400
      // "Every day" is a daily schedule, at a time of day.
      if (seconds === 86400 && count === 1) continue
      return count * seconds
    }
  }
  return null
}

function quantity(token: string, words: RequestWords): number | null {
  if (/^\d+$/.test(token)) return Number(token)
  return words.numbers[token] ?? words.numbers[token.replace(/[\s-]+/g, '')] ?? null
}

function parseWeekday(text: string, words: RequestWords): number | null {
  const { before, after, days, alone } = words.weekly
  const names = (list: string[][]) => `(?:${list.map((day, index) => `(?<d${index}>${phraseSource(day, words.spaced)})`).join('|')})`
  const forms = [
    pattern(words, 'weekBefore', () => (before.length && days.length ? `${phraseSource(before, words.spaced)}${gap(words)}${names(days)}` : null)),
    pattern(words, 'weekAfter', () => (after.length && days.length ? `${names(days)}\\s*${phraseSource(after, words.spaced, { start: false })}` : null)),
    pattern(words, 'weekAlone', () => (alone?.length ? names(alone) : null)),
  ]
  for (const form of forms) {
    const groups = form?.exec(text)?.groups
    if (!groups) continue
    for (let index = 0; index < 7; index++) {
      if (groups[`d${index}`] !== undefined) return index
    }
  }
  return null
}

const DAYPARTS: Daypart[] = ['morning', 'afternoon', 'evening', 'night']

/** The time of day a daily phrase names, "day" when it names none, or null when the request is not daily. */
function dailyPart(text: string, words: RequestWords): Daypart | 'day' | null {
  for (const part of DAYPARTS) {
    if (hasPhrase(text, words.daily[part], words.spaced)) return part
  }
  return hasPhrase(text, words.daily.day, words.spaced) ? 'day' : null
}

/** A time of day the request mentions, checked from evening to morning. */
function daypartIn(text: string, words: RequestWords): Daypart | null {
  for (const part of ['evening', 'night', 'afternoon', 'morning'] as const) {
    if (hasPhrase(text, words.dayparts[part], words.spaced)) return part
  }
  return null
}

function namedDaypart(part: Daypart): { hour: number; minute: number; note: string } {
  if (part === 'evening') return { hour: 18, minute: 0, note: i18n.t('automations:parse.evening') }
  if (part === 'night') return { hour: 21, minute: 0, note: i18n.t('automations:parse.night') }
  if (part === 'afternoon') return { hour: 15, minute: 0, note: i18n.t('automations:parse.afternoon') }
  return { hour: 8, minute: 0, note: i18n.t('automations:parse.morning') }
}

function onceDay(text: string, words: RequestWords): 'today' | 'tomorrow' | null {
  const { once, today, tomorrow, notTomorrow } = words.once
  const masked = notTomorrow?.length ? text.replace(phrases(notTomorrow, words.spaced, 'g') ?? /$^/, ' ') : text
  const isToday = hasPhrase(text, today, words.spaced)
  const isTomorrow = hasPhrase(masked, tomorrow, words.spaced)
  if (!isToday && !isTomorrow && !hasPhrase(text, once, words.spaced)) return null
  return isToday && !isTomorrow ? 'today' : 'tomorrow'
}

// A suffix that follows a number: "18h30", "8時", "6시에".
function suffix(list: string[], words: RequestWords): string {
  const source = phraseSource(list, words.spaced, { start: false, end: false })
  return words.spaced ? `${source}(?!\\p{L})` : `${source}(?![間间간])`
}

// 18:30, 18 h 30, 18h, 8 Uhr, 8時30分, 6시 반, 6点半
function timeSource(words: RequestWords): string {
  const hour = suffix(words.clock.hour, words)
  const minute = suffix(words.clock.minute, words)
  const half = suffix(words.clock.half, words)
  return `(?<![\\d.,:])(?<h>\\d{1,2})(?:(?::(?<m>\\d{2}))(?:\\s*${hour})?|\\s*${hour}(?:\\s*(?<m2>\\d{1,2})\\s*${minute}|\\s*(?<m3>\\d{2})(?!\\d)|\\s*(?<half>${half}))?)`
}

function parseClock(text: string, words: RequestWords): { hour: number; minute: number } | null {
  const { clock, spaced } = words
  const twelveHour = clock.am.length > 0 && clock.pm.length > 0
  const meridiem = `(?:(?<am>${phraseSource(clock.am, spaced)})|(?<pm>${phraseSource(clock.pm, spaced)}))`
  const forms = [
    pattern(words, 'clockMeridiemFirst', () => (twelveHour && clock.meridiemFirst ? `${meridiem}\\s*${timeSource(words)}` : null)),
    pattern(words, 'clockMeridiemLast', () =>
      twelveHour && !clock.meridiemFirst
        ? `(?<![\\d.,:])(?<h>\\d{1,2})(?::(?<m>\\d{2}))?\\s*(?:${suffix(clock.hour, words)}\\s*)?${meridiem}`
        : null,
    ),
    pattern(words, 'clockTime', () => timeSource(words)),
    pattern(words, 'clockAt', () =>
      clock.at.length
        ? `${phraseSource(clock.at, spaced)}\\s*(?<h>\\d{1,2})(?:\\s*(?<half>${suffix(clock.half, words)}))?(?!\\d|:|[.,]\\d)`
        : null,
    ),
  ]
  for (const form of forms) {
    const groups = form?.exec(text)?.groups
    if (!groups) continue
    const hour = Number(groups.h)
    const minute = Number(groups.m ?? groups.m2 ?? groups.m3 ?? (groups.half ? 30 : 0))
    if (groups.am !== undefined || groups.pm !== undefined) {
      return clockFrom(hour, minute, groups.pm !== undefined)
    }
    if (hour > 23 || minute > 59) return null
    return { hour: afterDaypart(hour, text, words), minute }
  }
  return null
}

// "8 in the evening" is 20:00: a time of day moves a morning hour to the afternoon.
function afterDaypart(hour: number, text: string, words: RequestWords): number {
  if (hour < 1 || hour > 11) return hour
  const part = daypartIn(text, words)
  if (part === 'afternoon' || part === 'evening') return hour + 12
  if (part === 'night' && hour >= 6) return hour + 12
  return hour
}

function clockFrom(hour: number, minute: number, pm: boolean): { hour: number; minute: number } {
  let next = hour % 12
  if (pm) next += 12
  if (minute > 59 || hour > 12 || hour < 1) {
    throw new Error(i18n.t('automations:parse.badTime'))
  }
  return { hour: next, minute }
}

function parseNotification(text: string, order: RequestWords[], currency: string): AutomationNotification {
  const says = (list: (words: RequestWords) => string[]) => order.some((words) => hasPhrase(text, list(words), words.spaced))
  if (says((words) => words.notify.none)) {
    return { mode: 'none' }
  }
  if (says((words) => words.notify.change)) {
    return { mode: 'change' }
  }
  if (says((words) => words.notify.significant)) {
    return { mode: 'condition', condition: { kind: 'significant' } }
  }
  for (const words of order) {
    const threshold = findThreshold(text, words)
    if (!threshold) continue
    const amount = readAmount(threshold.amount, order)
    if (!amount) continue
    return {
      mode: 'condition',
      condition: { kind: 'threshold', op: threshold.op, value: amount.value, currency: amount.currency ?? currency },
    }
  }
  if (says((words) => words.notify.available)) {
    return { mode: 'condition', condition: { kind: 'available' } }
  }
  return { mode: 'always' }
}

function findThreshold(text: string, words: RequestWords): { op: 'below' | 'above'; amount: string } | null {
  const { below, above, belowAfter, aboveAfter } = words.notify
  const amount = `(?<amount>${amountSource()})`
  const forms = [
    pattern(words, 'priceBefore', () =>
      below.length || above.length
        ? `(?:(?<below>${phraseSource(below, words.spaced)})|(?<above>${phraseSource(above, words.spaced)}))\\s*${amount}`
        : null,
    ),
    pattern(words, 'priceAfter', () =>
      belowAfter.length || aboveAfter.length
        ? `(?<![\\d.,])${amount}\\s*(?:(?<below>${phraseSource(belowAfter, words.spaced, { start: false })})|(?<above>${phraseSource(aboveAfter, words.spaced, { start: false })}))`
        : null,
    ),
  ]
  for (const form of forms) {
    const groups = form?.exec(text)?.groups
    if (groups?.amount) return { op: groups.above !== undefined ? 'above' : 'below', amount: groups.amount }
  }
  return null
}

function readableTask(original: string, notification: AutomationNotification, words: RequestWords): string {
  let text = original.trim().replace(/[.。]+$/, '')
  text = withoutScheduleClause(text, words)
  for (const ending of words.task.dropAtEnd) {
    text = text.replace(new RegExp(`${phraseSource([ending], words.spaced)}$`, 'iu'), '')
  }
  text = text.replace(/[\s,，、:：]+$/, '').replace(/\s+/g, ' ').trim()
  if (!text) text = original.trim()
  const stop = words.task.fullStop
  if (notification.condition?.kind === 'threshold' && !hasPhrase(normalizeRequest(text), words.task.price, words.spaced)) {
    text = `${text}${stop}${words.task.reportPrice}`
  }
  const sentence = text.charAt(0).toUpperCase() + text.slice(1)
  return /[.!?。！？]$/.test(sentence) ? sentence : `${sentence}${stop.trim()}`
}

// "Every morning at 8:00 AM, check this product" runs "Check this product":
// a leading clause that only says when is dropped from the task.
function withoutScheduleClause(text: string, words: RequestWords): string {
  // A colon ends a clause only before a space, so 8:00 stays a time.
  const comma = text.search(/[,，、：]|:(?=\s)/)
  if (comma <= 0 || comma > 80) return text
  const clause = text.slice(0, comma)
  const rest = text.slice(comma + 1).trim()
  if (!rest || /[.!?。！？]/.test(clause)) return text
  const normalized = withDigits(normalizeRequest(clause), words)
  try {
    if (!parseSchedule(normalized, words, new Date(0), 'UTC')) return text
  } catch {
    return text
  }
  return onlySchedule(normalized, words) ? rest : text
}

// Whether a clause says nothing but when: "Täglich um 7:30 Uhr prüfen" also
// says what to do, so it stays. Short words such as "de la", に, and 에 may remain.
function onlySchedule(clause: string, words: RequestWords): boolean {
  const scheduleWords = pattern(
    words,
    'scheduleWords',
    () => {
      const { interval, daily, dayparts, weekly, once, clock } = words
      const lists = [
        interval.before,
        interval.after,
        ...Object.values(interval.units),
        interval.hourly,
        interval.halfHour,
        Object.keys(words.numbers),
        ...Object.values(daily),
        ...Object.values(dayparts),
        weekly.before,
        weekly.after,
        ...weekly.days,
        ...(weekly.alone ?? []),
        once.once,
        once.today,
        once.tomorrow,
        clock.at,
        clock.hour,
        clock.minute,
        clock.half,
        clock.am,
        clock.pm,
      ]
      return `\\d+(?:[:.]\\d+)?|${phraseSource(lists.flat(), words.spaced)}`
    },
    'gu',
  )
  const rest = scheduleWords ? clause.replace(scheduleWords, ' ') : clause
  return rest
    .split(/[\s\p{P}]+/u)
    .filter(Boolean)
    .every((word) => [...word].length <= 3)
}

function automationName(text: string, original: string, notification: AutomationNotification, order: RequestWords[]): string {
  const says = (list: (words: RequestWords) => string[]) => order.some((words) => hasPhrase(text, list(words), words.spaced))
  const condition = notification.condition
  if (notification.mode === 'condition' && condition?.kind === 'threshold') {
    const amount = formatAmount(condition.value ?? 0, condition.currency)
    return i18n.t(condition.op === 'above' ? 'automations:names.priceAbove' : 'automations:names.priceBelow', { amount })
  }
  if (condition?.kind === 'available') {
    return says((words) => words.names.stock) ? i18n.t('automations:names.stock') : i18n.t('automations:names.availability')
  }
  if (condition?.kind === 'significant') return i18n.t('automations:names.significance')
  if (says((words) => words.names.release)) return i18n.t('automations:names.release')
  if (says((words) => words.names.research)) return i18n.t('automations:names.research')
  const cleaned = original.replace(/\s+/g, ' ').trim()
  if (!cleaned) return i18n.t('automations:names.scheduled')
  const short = cleaned.length > 48 ? `${cleaned.slice(0, 48).trim()}…` : cleaned
  return short.charAt(0).toUpperCase() + short.slice(1)
}

// visibleTask removes the machine-readable result instruction from a stored prompt.
export function visibleTask(prompt: string): string {
  let text = prompt.trim()
  text = text.replace(PRICE_INSTRUCTION_LINE, '')
  for (const line of [AVAILABLE_INSTRUCTION, SIGNIFICANT_INSTRUCTION]) {
    text = text.split(line).join('')
  }
  return text.replace(/\n{3,}/g, '\n\n').trim()
}

// composePrompt stores the task the user wrote and, when needed, the result instruction.
export function composePrompt(task: string, notification: AutomationNotification): string {
  return withSignalInstruction(visibleTask(task), notification)
}

export function resultProse(result: string | undefined): string {
  if (!result) return ''
  const withoutInstruction = visibleTask(result)
  return withoutInstruction
    .replace(/```json[\s\S]*?```/g, '')
    .replace(/\{[^{}]*"(?:price|available|significant)"[^{}]*\}/g, '')
    .replace(/\n{3,}/g, '\n\n')
    .trim()
}

function withSignalInstruction(text: string, notification: AutomationNotification): string {
  const instruction = signalInstruction(notification)
  if (!instruction || text.includes('{"price"') || text.includes('{"available"') || text.includes('{"significant"')) {
    return text.trim()
  }
  return `${text.trim()}\n\n${instruction}`
}

function signalInstruction(notification: AutomationNotification): string {
  switch (notification.condition?.kind) {
    case 'threshold':
      return priceInstruction(notification.condition.currency)
    case 'available':
      return AVAILABLE_INSTRUCTION
    case 'significant':
      return SIGNIFICANT_INSTRUCTION
    default:
      return ''
  }
}

function formatAmount(value: number, currency: string | undefined): string {
  return formatPrice(value, currency)
}

function conditionLabel(condition: AutomationCondition | undefined): string {
  if (!condition) return i18n.t('automations:notify.condition')
  if (condition.kind === 'threshold') {
    return i18n.t(condition.op === 'above' ? 'automations:notify.priceAbove' : 'automations:notify.priceBelow', {
      amount: formatAmount(condition.value ?? 0, condition.currency),
    })
  }
  if (condition.kind === 'available') return i18n.t('automations:notify.available')
  return i18n.t('automations:notify.significant')
}

function intervalLabel(seconds: number): string {
  if (seconds <= 0) return i18n.t('automations:schedule.onInterval')
  if (seconds % 86400 === 0) {
    const days = seconds / 86400
    return days === 1 ? i18n.t('automations:schedule.everyDay') : i18n.t('automations:schedule.days', { count: days })
  }
  if (seconds % 3600 === 0) {
    const hours = seconds / 3600
    return hours === 1 ? i18n.t('automations:schedule.everyHour') : i18n.t('automations:schedule.hours', { count: hours })
  }
  if (seconds % 60 === 0) {
    const minutes = seconds / 60
    return minutes === 1 ? i18n.t('automations:schedule.everyMinute') : i18n.t('automations:schedule.minutes', { count: minutes })
  }
  return i18n.t('automations:schedule.seconds', { count: seconds })
}

/** A time of day in the App language's clock: 6:30 PM in English, 18:30 in German. */
function clockLabel(hour: number, minute: number): string {
  return formatDate(new Date(2000, 0, 1, hour, minute), { hour: 'numeric', minute: '2-digit' })
}

interface CivilParts {
  year: number
  month: number
  day: number
  hour: number
  minute: number
}

function zonedParts(date: Date, timeZone: string): CivilParts {
  const fmt = new Intl.DateTimeFormat('en-US', {
    timeZone,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hourCycle: 'h23',
  })
  const parts = Object.fromEntries(fmt.formatToParts(date).map((part) => [part.type, part.value]))
  return {
    year: Number(parts.year),
    month: Number(parts.month),
    day: Number(parts.day),
    hour: Number(parts.hour) % 24,
    minute: Number(parts.minute),
  }
}

function addDays(parts: CivilParts, days: number): CivilParts {
  const next = new Date(Date.UTC(parts.year, parts.month - 1, parts.day + days))
  return {
    year: next.getUTCFullYear(),
    month: next.getUTCMonth() + 1,
    day: next.getUTCDate(),
    hour: parts.hour,
    minute: parts.minute,
  }
}

function instantInZone(year: number, month: number, day: number, hour: number, minute: number, timeZone: string): Date {
  const utcGuess = Date.UTC(year, month - 1, day, hour, minute)
  const observed = zonedParts(new Date(utcGuess), timeZone)
  const observedUTC = Date.UTC(observed.year, observed.month - 1, observed.day, observed.hour, observed.minute)
  return new Date(utcGuess - (observedUTC - utcGuess))
}
