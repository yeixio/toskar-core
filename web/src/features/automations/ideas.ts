import i18n from '@/i18n'
import type { AutomationCamera, AutomationNotification, AutomationSay, AutomationSchedule, AutomationTrigger, Device, DeviceConnector } from '@/types/api'

/**
 * Ready-made automations to start from (#204). Each is a template with a
 * few fields, such as a product link and a price, that fills in the form
 * directly: automations:ideas.<id> in the catalog has its title, a line on
 * what you get, its name and task with the fields in them, and an example
 * request to describe it in your own words instead.
 * internal/automations/request_test.go checks every language's example
 * requests read as intended.
 */
export type IdeaId = 'news' | 'price' | 'stock' | 'releases' | 'page' | 'folder' | 'door'

export type IdeaField = 'topics' | 'url' | 'price' | 'currency' | 'software' | 'folder' | 'time' | 'weekday'

export interface Idea {
  id: IdeaId
  /** The fields the person fills in, in order; the first ones are required. */
  fields: IdeaField[]
  required: IdeaField[]
  /** Offered only with a doorbell connected (#1341). */
  doorbell?: boolean
}

export const IDEAS: Idea[] = [
  { id: 'news', fields: ['topics', 'time'], required: ['topics'] },
  { id: 'price', fields: ['url', 'price', 'currency', 'time'], required: ['url', 'price'] },
  { id: 'stock', fields: ['url'], required: ['url'] },
  { id: 'releases', fields: ['software', 'weekday', 'time'], required: ['software'] },
  { id: 'page', fields: ['url', 'time'], required: ['url'] },
  { id: 'folder', fields: ['folder', 'time'], required: ['folder'] },
  { id: 'door', fields: [], required: [], doorbell: true },
]

/** Whether a device is a doorbell's button: someone pressed it (#1339, #1341). */
export function isRing(device: string | undefined): boolean {
  const d = (device ?? '').toLowerCase()
  return d.includes('doorbell') || d.includes('visitor')
}

/** The doorbell button and the camera beside it, among the connected devices. */
export function doorbellOf(devices: Device[]): { ring: Device; camera?: Device } | null {
  const ring = devices.find((d) => d.kind !== 'camera' && isRing(d.device))
  if (!ring) return null
  const camera = devices.find((d) => d.kind === 'camera' && d.connector === ring.connector) ?? devices.find((d) => d.kind === 'camera')
  return { ring, camera }
}

/** The ideas to offer: Answer the door only with a doorbell connected (#1341). */
export function availableIdeas(devices: Device[]): Idea[] {
  const door = doorbellOf(devices) !== null
  return IDEAS.filter((idea) => !idea.doorbell || door)
}

export type IdeaValues = Partial<Record<IdeaField, string>>

// A threshold in the App language's usual currency, as requests read one.
const LANGUAGE_CURRENCY: Record<string, string> = {
  en: 'USD', de: 'EUR', es: 'EUR', fr: 'EUR', it: 'EUR', ja: 'JPY', ko: 'KRW', 'pt-BR': 'BRL', 'zh-Hans': 'CNY', 'zh-Hant': 'TWD',
}

/** What a template's fields start as. */
export function ideaDefaults(id: IdeaId): IdeaValues {
  const language = i18n.resolvedLanguage ?? i18n.language
  const times: Record<IdeaId, string> = { news: '07:30', price: '08:00', stock: '', releases: '09:00', page: '18:00', folder: '18:00', door: '' }
  return {
    topics: id === 'news' ? i18n.t('automations:ideas.news.defaultTopics') : '',
    currency: LANGUAGE_CURRENCY[language] ?? 'USD',
    time: times[id],
    weekday: '5',
  }
}

/** Whether every required field has something in it. */
export function ideaReady(idea: Idea, values: IdeaValues): boolean {
  return idea.required.every((field) => (values[field] ?? '').trim() !== '')
}

export interface IdeaAutomation {
  name: string
  prompt: string
  schedule: AutomationSchedule
  notification: AutomationNotification
  /** What it watches, for a template that runs when something changed. */
  trigger?: AutomationTrigger
  /** The cameras it looks at, and speaking at the door (#1341). */
  cameras?: AutomationCamera[]
  say?: AutomationSay
}

/** The automation a template's fields make, in the App language. */
export function buildIdea(idea: Idea, values: IdeaValues, timeZone: string, readPrice: (text: string) => number | null, devices: Device[] = []): IdeaAutomation {
  const url = withScheme((values.url ?? '').trim())
  const params = {
    url,
    site: siteOf(url),
    topics: (values.topics ?? '').trim(),
    software: (values.software ?? '').trim(),
    folder: (values.folder ?? '').trim(),
    folderName: baseName((values.folder ?? '').trim()),
    door: '',
  }
  const [hour, minute] = (values.time || '08:00').split(':').map(Number)
  const at = { hour: hour || 0, minute: minute || 0 }
  const daily: AutomationSchedule = { kind: 'daily', time_zone: timeZone, ...at, times: [at] }
  let schedule = daily
  let notification: AutomationNotification = { mode: 'always' }
  let trigger: AutomationTrigger | undefined
  let cameras: AutomationCamera[] | undefined
  let say: AutomationSay | undefined
  switch (idea.id) {
    case 'price':
      notification = {
        mode: 'condition',
        condition: { kind: 'threshold', op: 'below', value: readPrice((values.price ?? '').trim()) ?? Number.NaN, currency: values.currency || 'USD' },
      }
      break
    case 'stock':
      schedule = { kind: 'interval', time_zone: timeZone, every_seconds: 6 * 3600 }
      notification = { mode: 'condition', condition: { kind: 'available' } }
      break
    case 'releases': {
      const day = Number(values.weekday ?? 5)
      schedule = { kind: 'weekly', time_zone: timeZone, ...at, times: [at], weekday: day, weekdays: [day] }
      break
    }
    case 'page':
      // Watched, as its summary says: it runs when the page changes,
      // checked at the time chosen (#1269).
      notification = { mode: 'change' }
      trigger = { kind: 'page', url }
      break
    case 'folder':
      // The run can't open files outside Toskar's workspace, so the folder
      // is watched: it runs, given what changed, only when something did.
      trigger = { kind: 'folder', path: params.folder }
      break
    case 'door': {
      // The doorbell's button starts it at once; it looks at the camera and
      // talks with whoever is there (#1341).
      const door = doorbellOf(devices)
      if (!door) break
      schedule = { kind: 'interval', time_zone: timeZone, every_seconds: 3600 }
      trigger = { kind: 'device', connector: door.ring.connector as DeviceConnector, device: door.ring.device, cooldown_seconds: 300 }
      params.door = door.camera?.name ?? door.ring.name
      if (door.camera) {
        cameras = [{ connector: door.camera.connector as DeviceConnector, device: door.camera.device }]
        if (door.camera.connector === 'reolink') say = { connector: 'reolink', device: 'camera', listen: true }
      }
      break
    }
  }
  return {
    name: i18n.t(`automations:ideas.${idea.id}.name`, params),
    prompt: i18n.t(`automations:ideas.${idea.id}.task`, params),
    schedule,
    notification,
    trigger,
    cameras,
    say,
  }
}

function withScheme(url: string): string {
  if (!url || /^[a-z][a-z0-9+.-]*:\/\//i.test(url)) return url
  return `https://${url}`
}

function siteOf(url: string): string {
  try {
    return new URL(url).hostname.replace(/^www\./, '')
  } catch {
    return url
  }
}

function baseName(path: string): string {
  const parts = path.replace(/[\\/]+$/, '').split(/[\\/]/)
  return parts[parts.length - 1] || path
}
