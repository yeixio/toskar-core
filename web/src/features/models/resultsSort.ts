/** Names in the reader's language's order. */
export function compareAlphabetical(a: string, b: string): number {
  return a.localeCompare(b, undefined, { numeric: true, sensitivity: 'base' })
}

/** The sets a Score averages. */
const SCORE_SETS = ['quality', 'topics', 'files', 'reasoning']

/**
 * A row's Score, from 0 to 100: the plain average of the pass rates of the four
 * sets, or null for a row missing any (docs/quality.md). Worked out when shown,
 * so a new formula rewrites no data, and speed stays out of it.
 */
export function overallScore(scores: Record<string, { passed: number; of: number }>): number | null {
  const rates = SCORE_SETS.map((set) => scores[set])
  if (rates.some((s) => !s || !s.of)) return null
  return Math.round((100 * rates.reduce((sum, s) => sum + s.passed / s.of, 0)) / rates.length)
}
