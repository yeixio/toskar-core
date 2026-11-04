// Terminal control codes: colors and cursor moves, window titles, and the rest.
// eslint-disable-next-line no-control-regex
const CONTROL_CODES = /\x1b\[[0-?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)?|\x1b[@-Z\\^_]/g
// eslint-disable-next-line no-control-regex
const OTHER_CONTROLS = /[\x00-\x08\x0b\x0c\x0e-\x1f\x7f]/g

/** How much of a running command's output is kept. */
export const OUTPUT_LIMIT = 16 * 1024

/** Adds what a command just wrote, keeping only the end of it. */
export function addOutput(current: string, text: string): string {
  const next = current + text
  return next.length > OUTPUT_LIMIT ? next.slice(next.length - OUTPUT_LIMIT) : next
}

/**
 * A command's output as it reads on a terminal, without control codes: what
 * came after a carriage return replaces its line, so a progress bar shows once.
 */
export function plainOutput(text: string): string {
  return text
    .replace(CONTROL_CODES, '')
    .split('\n')
    .map((line) => {
      const trimmed = line.replace(/\r+$/, '')
      return trimmed.slice(trimmed.lastIndexOf('\r') + 1)
    })
    .join('\n')
    .replace(OTHER_CONTROLS, '')
}

/** The end of a folder's path, to name it in little room. */
export function folderName(path: string): string {
  const parts = path.split(/[\\/]+/).filter(Boolean)
  return parts[parts.length - 1] ?? path
}
