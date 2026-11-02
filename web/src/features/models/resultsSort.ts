/** Names in the reader's language's order. */
export function compareAlphabetical(a: string, b: string): number {
  return a.localeCompare(b, undefined, { numeric: true, sensitivity: 'base' })
}
