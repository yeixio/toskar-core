import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

// Every setting has a row in docs/settings-inventory.md (#1374, section 10 of
// docs/navigation-and-settings.md), so a new setting gets a deliberate place.
const root = resolve(__dirname, '../../../..')

function settingsPatchKeys(): string[] {
  const types = readFileSync(resolve(root, 'web/src/types/api.ts'), 'utf8')
  const start = types.indexOf('export interface SettingsPatch {')
  const end = types.indexOf('\n}', start)
  const body = types.slice(start, end)
  return [...body.matchAll(/^\s+([a-z_]+)\?:/gm)].map((m) => m[1])
}

describe('settings inventory', () => {
  it('lists every field of SettingsPatch', () => {
    const inventory = readFileSync(resolve(root, 'docs/settings-inventory.md'), 'utf8')
    const keys = settingsPatchKeys()
    expect(keys.length).toBeGreaterThan(30)
    const missing = keys.filter((key) => !inventory.includes(`\`${key}\``))
    expect(missing, 'add a row for each to docs/settings-inventory.md').toEqual([])
  })
})
