/**
 * The Norse name and Elder Futhark rune for each page. Tab labels follow the
 * App language; the Norse name is a small heading above each page title.
 *
 * Pages backed by a subsystem use its name (see designSystem.ts). The rest
 * take a figure whose story fits the page.
 */

export type RuneId =
  | 'ansuz'
  | 'jera'
  | 'uruz'
  | 'kenaz'
  | 'laguz'
  | 'othala'
  | 'raidho'
  | 'ehwaz'
  | 'algiz'
  | 'mannaz'
  | 'tiwaz'
  | 'thurisaz'
  | 'isa'
  | 'gebo'
  | 'wunjo'
  | 'dagaz'

export interface Realm {
  /**
   * The realm's key in the lore catalog (i18n/locales/<language>/lore.json):
   * realms.<id>.meaning is the page's job in one sentence, realms.<id>.story
   * who or what this is in the myths, and realms.<id>.rune the rune's name.
   */
  id: string
  /** Norse name shown above the page title; the same in every language. */
  norse: string
  rune: RuneId
  /** Text color class for the name and the active rune. */
  accent: string
}

export const realms: Record<string, Realm> = {
  '/chat': {
    id: 'chat',
    norse: 'Ratatoskr',
    rune: 'ansuz',
    accent: 'text-ratatoskr',
  },
  '/automations': {
    id: 'automations',
    norse: 'Norn',
    rune: 'jera',
    accent: 'text-norn',
  },
  '/models': {
    id: 'models',
    norse: 'Ymir',
    rune: 'uruz',
    accent: 'text-ygg',
  },
  '/train': {
    id: 'train',
    norse: 'Brokkr',
    rune: 'kenaz',
    accent: 'text-gungnir',
  },
  '/knowledge': {
    id: 'knowledge',
    norse: 'Mimir',
    rune: 'laguz',
    accent: 'text-mimir',
  },
  '/memory': {
    id: 'memory',
    norse: 'Muninn',
    rune: 'othala',
    accent: 'text-muninn',
  },
  // AI news (#750): Huginn, Odin's raven of thought, who flies over the
  // world each morning and tells him what he saw; Dagaz, the rune of day.
  '/news': {
    id: 'news',
    norse: 'Huginn',
    rune: 'dagaz',
    accent: 'text-huginn',
  },
  // People share this Toskar (#206): Midgard, the world of people, and
  // Gebo, the rune of the gift and of fellowship.
  '/people': {
    id: 'people',
    norse: 'Midgard',
    rune: 'gebo',
    accent: 'text-bifrost',
  },
  // Chat portals (#205): Valhalla, the hall that welcomes its guests, and
  // Wunjo, the rune of joy and of welcome.
  '/portals': {
    id: 'portals',
    norse: 'Valhalla',
    rune: 'wunjo',
    accent: 'text-bifrost',
  },
  '/nodes': {
    id: 'nodes',
    norse: 'Bifrost',
    rune: 'raidho',
    accent: 'text-bifrost',
  },
  '/performance': {
    id: 'performance',
    norse: 'Sleipnir',
    rune: 'ehwaz',
    accent: 'text-huginn',
  },
  '/diagnostics': {
    id: 'diagnostics',
    norse: 'Heimdall',
    rune: 'algiz',
    accent: 'text-heimdall',
  },
  '/profiles': {
    id: 'profiles',
    norse: 'Odin',
    rune: 'mannaz',
    accent: 'text-norn',
  },
  '/tools': {
    id: 'tools',
    norse: 'Gungnir',
    rune: 'tiwaz',
    accent: 'text-gungnir',
  },
  '/api-access': {
    id: 'api-access',
    norse: 'Valgrind',
    rune: 'thurisaz',
    accent: 'text-bifrost',
  },
  '/settings': {
    id: 'settings',
    norse: 'Forseti',
    rune: 'isa',
    accent: 'text-huginn',
  },
}

/** The realm for a path such as /knowledge or /knowledge/abc. */
export function realmFor(pathname: string): Realm | undefined {
  const base = '/' + (pathname.split('/')[1] ?? '')
  return realms[base]
}

/**
 * Rune strokes on a 10 × 16 grid. Elder Futhark runes are made of straight
 * lines, so a few path commands draw each one crisply at any size, without
 * depending on a font that has the Runic block.
 */
export const runePaths: Record<RuneId, string> = {
  ansuz: 'M3 15V1M3 1l5 3.5M3 5l5 3.5',
  jera: 'M5.5 2 2 6l3.5 4M4.5 6 8 10l-3.5 4',
  uruz: 'M2 15V1l6 4v10',
  kenaz: 'M8 3 3 8l5 5',
  laguz: 'M3 15V1l5 4',
  othala: 'M2 13 8 5 5 1.5 2 5l6 8',
  raidho: 'M3 15V1l5 3.5L3 8l5 7',
  ehwaz: 'M2 15V1l3 4 3-4v14',
  algiz: 'M5 15V1M5 7 1.5 2M5 7l3.5-5',
  gebo: 'M1 1l8 14M9 1L1 15',
  mannaz: 'M2 15V1l6 6V1v14M2 7l6-6',
  tiwaz: 'M5 15V1M1.5 5 5 1l3.5 4',
  thurisaz: 'M3 1v14M3 4l5 4-5 4',
  isa: 'M5 1v14',
  wunjo: 'M3 15V1l5 3.5L3 8',
  dagaz: 'M1 2v12M9 2v12M1 2l8 12M9 2 1 14',
}
