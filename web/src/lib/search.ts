// Search boxes that forgive typos (#451): "mistrl" finds Mistral,
// "atomations" finds Automations. What a search found before it still finds,
// and it comes first; a near miss is added after it.

/** How a text matched a search: exactly (the search is in it), with typos, or not at all. */
export type SearchMatch = 'exact' | 'near' | 'none'

const wordRe = /[\p{L}\p{N}]+/gu

/**
 * Whether the text in fields matches what was typed. Exact is the old check:
 * the whole search is in the text. Near is every word of the search either
 * in the text or a typo away from the start of one of its words: one edit
 * for 4 to 7 letters, two for 8 or more, none for 3 or fewer, so a short
 * search stays exact. Comparing with a word's start lets a search match as
 * it's typed ("atomat" for "automations").
 */
export function searchMatch(query: string, ...fields: (string | undefined | null)[]): SearchMatch {
  const needle = query.trim().toLowerCase()
  if (!needle) return 'exact'
  const text = fields.filter(Boolean).join(' ').toLowerCase()
  if (text.includes(needle)) return 'exact'
  const words = text.match(wordRe) ?? []
  const parts = needle.match(wordRe) ?? []
  if (parts.length === 0) return 'none'
  return parts.every((part) => text.includes(part) || words.some((word) => nearStart(part, word))) ? 'near' : 'none'
}

/** Whether text matches a search, exactly or with typos. */
export function matchesSearch(query: string, ...fields: (string | undefined | null)[]): boolean {
  return searchMatch(query, ...fields) !== 'none'
}

/** The items that match a search, exact matches first, each group in its own order. */
export function searchItems<T>(items: T[], query: string, fields: (item: T) => (string | undefined | null)[]): T[] {
  if (!query.trim()) return items
  const exact: T[] = []
  const near: T[] = []
  for (const item of items) {
    const m = searchMatch(query, ...fields(item))
    if (m === 'exact') exact.push(item)
    else if (m === 'near') near.push(item)
  }
  return [...exact, ...near]
}

/** Whether part is a typo away from word, or from its start. */
function nearStart(part: string, word: string): boolean {
  const size = [...part].length
  if (size <= 3) return false
  const limit = size >= 8 ? 2 : 1
  const letters = [...word]
  for (let n = Math.max(1, size - limit); n <= Math.min(letters.length, size + limit); n++) {
    if (distance(part, letters.slice(0, n).join(''), limit) <= limit) return true
  }
  return false
}

/** Edits (insert, delete, substitute, swap two neighbours) between a and b, counted up to limit + 1. */
export function distance(a: string, b: string, limit: number): number {
  const ra = [...a]
  const rb = [...b]
  if (Math.abs(ra.length - rb.length) > limit) return limit + 1
  let prev2 = new Array<number>(rb.length + 1).fill(0)
  let prev = Array.from({ length: rb.length + 1 }, (_, j) => j)
  let cur = new Array<number>(rb.length + 1).fill(0)
  for (let i = 1; i <= ra.length; i++) {
    cur[0] = i
    let rowMin = cur[0]
    for (let j = 1; j <= rb.length; j++) {
      const cost = ra[i - 1] === rb[j - 1] ? 0 : 1
      cur[j] = Math.min(prev[j] + 1, cur[j - 1] + 1, prev[j - 1] + cost)
      if (i > 1 && j > 1 && ra[i - 1] === rb[j - 2] && ra[i - 2] === rb[j - 1]) cur[j] = Math.min(cur[j], prev2[j - 2] + 1)
      rowMin = Math.min(rowMin, cur[j])
    }
    if (rowMin > limit) return limit + 1
    ;[prev2, prev, cur] = [prev, cur, prev2]
  }
  return Math.min(prev[rb.length], limit + 1)
}
