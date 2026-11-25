import type { Role } from '@/types/api'

/**
 * Settings' categories (docs/settings-inventory.md section 6, #1374, #1321):
 * "You" holds what is each person's own, "This computer and team" what runs
 * the computer for everyone. Labels are keys in the settings namespace;
 * keywords are the names of the settings on each page, for the search.
 */
export interface SettingsNavItem {
  /** The path under /settings. */
  path: string
  label: string
  /** The least role that sees it; Visitors see what has none. */
  min?: Extract<Role, 'member' | 'admin'>
  /** Setting names on the page, as keys in the settings namespace (or ns:key). */
  keywords?: string[]
}

export interface SettingsNavCategory {
  /** A category with pages under it shows its label as a heading. */
  label?: string
  items: SettingsNavItem[]
}

export interface SettingsNavGroup {
  label: string
  min?: Extract<Role, 'member' | 'admin'>
  categories: SettingsNavCategory[]
}

export const settingsNav: SettingsNavGroup[] = [
  {
    label: 'settings:nav.you',
    categories: [
      { items: [{ path: 'account', label: 'sections.account', keywords: ['devices.title'] }] },
      { items: [{ path: 'general', label: 'sections.general', keywords: ['language.title', 'background.title', 'launchAtLogin.title'] }] },
      { items: [{ path: 'appearance', label: 'sections.appearance', keywords: ['appearance.dark', 'appearance.light'] }] },
      {
        items: [
          { path: 'personalization', label: 'sections.personalization', keywords: ['history.suggestModels'] },
          { path: 'memory', label: 'sections.memory', min: 'member' },
        ],
      },
      { items: [{ path: 'notifications', label: 'sections.notifications', min: 'admin', keywords: ['notifications.taskFinish', 'notifications.peerOffline', 'notifications.newerModels'] }] },
      { items: [{ path: 'voice', label: 'sections.voice', min: 'admin' }] },
    ],
  },
  {
    label: 'settings:nav.computer',
    min: 'admin',
    categories: [
      { items: [{ path: 'features', label: 'sections.features', keywords: ['features.items.voice.title', 'features.items.pictures.title', 'features.items.meaning.title'] }] },
      {
        items: [
          {
            path: 'models',
            label: 'sections.models',
            keywords: ['defaultProfile.title', 'runOn.title', 'lifecycle.title', 'downloads.title', 'ratings.title', 'storage.limit', 'storage.folders'],
          },
        ],
      },
      { items: [{ path: 'history', label: 'sections.history', keywords: ['history.saveChats', 'history.saveTasks', 'history.compactAt'] }] },
      {
        label: 'sections.networkDevices',
        items: [
          { path: 'computers', label: 'sections.computers', keywords: ['discovery.title'] },
          { path: 'network', label: 'sections.network', keywords: ['apiAccess:lan.title', 'apiAccess:remote.title', 'apiAccess:phones.title'] },
        ],
      },
      {
        label: 'sections.peoplePortals',
        items: [
          { path: 'people', label: 'sections.people' },
          { path: 'portals', label: 'sections.portals' },
        ],
      },
      {
        label: 'sections.connections',
        items: [
          { path: 'tools', label: 'sections.tools' },
          { path: 'apps', label: 'sections.apps', keywords: ['apiAccess:share.title'] },
        ],
      },
      {
        items: [
          {
            path: 'privacy',
            label: 'sections.privacy',
            keywords: ['whatLeft.title', 'toolPermissions.title', 'links.title', 'reports.title', 'news.title'],
          },
        ],
      },
      {
        label: 'sections.advancedGroup',
        items: [
          { path: 'advanced', label: 'sections.advanced', keywords: ['advanced.title', 'reset.title'] },
          { path: 'performance', label: 'sections.performance' },
          { path: 'diagnostics', label: 'sections.diagnostics' },
          { path: 'developer', label: 'sections.developer', keywords: ['apiAccess:keys.title'] },
        ],
      },
      { items: [{ path: 'about', label: 'sections.about', keywords: ['about.thisComputer'] }] },
    ],
  },
]

/** Whether someone with this role sees it; until the role is known, the Owner's view. */
export function visibleTo(min: SettingsNavItem['min'], atLeast: (role: Role) => boolean, known: boolean): boolean {
  return !min || !known || atLeast(min)
}

/** The items matching a search, by label or the settings on the page; every item when it's empty. */
export function matches(item: SettingsNavItem, query: string, t: (key: string) => string): boolean {
  const q = query.trim().toLocaleLowerCase()
  if (!q) return true
  return [item.label, ...(item.keywords ?? [])].some((key) => t(key).toLocaleLowerCase().includes(q))
}

/**
 * Where the pages that moved into Settings used to be, so links from
 * notices, onboarding, the docs and the apps still open them.
 */
export const movedPages: Record<string, string> = {
  '/memory': '/settings/memory',
  '/tools': '/settings/tools',
  '/nodes': '/settings/computers',
  '/people': '/settings/people',
  '/portals': '/settings/portals',
  '/api-access': '/settings/network',
  '/performance': '/settings/performance',
  '/diagnostics': '/settings/diagnostics',
  '/train': '/models/train',
}
