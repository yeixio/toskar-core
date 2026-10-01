/**
 * The Norse name and Elder Futhark rune for each page. Tab labels stay in
 * plain English; the Norse name is a small heading above each page title.
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

export interface Realm {
  /** Norse name shown above the page title. */
  norse: string
  /** What the name means here, for the tooltip. */
  meaning: string
  rune: RuneId
  /** Name of the rune, for the tooltip. */
  runeName: string
  /** Text color class for the name and the active rune. */
  accent: string
}

export const realms: Record<string, Realm> = {
  '/chat': {
    norse: 'Huginn',
    meaning: "Huginn, Odin's raven of thought, decides how each request is answered.",
    rune: 'ansuz',
    runeName: 'Ansuz, the rune of speech',
    accent: 'text-huginn',
  },
  '/automations': {
    norse: 'Norn',
    meaning: 'The Norns tend what is fated to happen and when. Norn runs scheduled work.',
    rune: 'jera',
    runeName: 'Jera, the rune of the turning year',
    accent: 'text-norn',
  },
  '/models': {
    norse: 'Ymir',
    meaning: 'Ymir is the first giant, from whom the world was made. Models are what everything else is built on.',
    rune: 'uruz',
    runeName: 'Uruz, the rune of raw strength',
    accent: 'text-ygg',
  },
  '/train': {
    norse: 'Brokkr',
    meaning: "Brokkr is the smith who forged Mjölnir. Here you forge an AI of your own.",
    rune: 'kenaz',
    runeName: 'Kenaz, the rune of the torch and craft',
    accent: 'text-gungnir',
  },
  '/knowledge': {
    norse: 'Mimir',
    meaning: "Mimir guards the well of wisdom. Mimir keeps your knowledge searchable.",
    rune: 'laguz',
    runeName: 'Laguz, the rune of water and the well',
    accent: 'text-mimir',
  },
  '/memory': {
    norse: 'Muninn',
    meaning: "Muninn, Odin's raven of memory, keeps what you ask Yggdrasil to remember.",
    rune: 'othala',
    runeName: 'Othala, the rune of what is kept',
    accent: 'text-muninn',
  },
  '/nodes': {
    norse: 'Bifrost',
    meaning: 'Bifrost is the rainbow bridge between worlds. It connects your computers.',
    rune: 'raidho',
    runeName: 'Raidho, the rune of the journey',
    accent: 'text-bifrost',
  },
  '/performance': {
    norse: 'Sleipnir',
    meaning: "Sleipnir is Odin's eight-legged horse, the fastest of all.",
    rune: 'ehwaz',
    runeName: 'Ehwaz, the rune of the horse',
    accent: 'text-huginn',
  },
  '/diagnostics': {
    norse: 'Heimdall',
    meaning: 'Heimdall watches over the bridge and misses nothing. He keeps watch on health.',
    rune: 'algiz',
    runeName: 'Algiz, the rune of protection',
    accent: 'text-heimdall',
  },
  '/profiles': {
    norse: 'Odin',
    meaning: 'Odin sends out his ravens. Profiles decide how the assistant thinks and what it may do.',
    rune: 'mannaz',
    runeName: 'Mannaz, the rune of the self',
    accent: 'text-norn',
  },
  '/tools': {
    norse: 'Gungnir',
    meaning: "Gungnir is Odin's spear, which never misses. Tools are how the assistant acts.",
    rune: 'tiwaz',
    runeName: 'Tiwaz, the spear-shaped rune',
    accent: 'text-gungnir',
  },
  '/api-access': {
    norse: 'Valgrind',
    meaning: 'Valgrind is the gate of Valhalla. API keys decide who may come in.',
    rune: 'thurisaz',
    runeName: 'Thurisaz, the rune of the gate',
    accent: 'text-bifrost',
  },
  '/settings': {
    norse: 'Forseti',
    meaning: 'Forseti settles every matter fairly. Settings are the rules Yggdrasil keeps.',
    rune: 'isa',
    runeName: 'Isa, the rune of stillness',
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
  mannaz: 'M2 15V1l6 6V1v14M2 7l6-6',
  tiwaz: 'M5 15V1M1.5 5 5 1l3.5 4',
  thurisaz: 'M3 1v14M3 4l5 4-5 4',
  isa: 'M5 1v14',
}
