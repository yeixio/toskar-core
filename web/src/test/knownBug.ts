import { it } from 'vitest'

/**
 * A regression test for a QA issue that isn't fixed yet.
 *
 * It runs the check every time and passes while the check fails, so CI stays
 * green. Once the bug is fixed the check passes and this test fails, so the
 * fix turns it into a plain `it(...)`, keeping the `Regression:` comment, and
 * from then on the bug can't come back unnoticed. The QA ledger
 * (.github/scripts/qa-ledger.sh) finds the test by the issue it names.
 *
 * `QA_KNOWN_BUGS=show pnpm test` runs these as plain tests, to see how each
 * one fails today.
 */
export function knownBug(issue: string, name: string, check: () => unknown) {
  const env = (globalThis as { process?: { env: Record<string, string | undefined> } }).process?.env
  const run = env?.QA_KNOWN_BUGS === 'show' ? it : it.fails
  run(`${issue}: ${name}`, check)
}
