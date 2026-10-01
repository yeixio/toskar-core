import { describe, expect, it } from 'vitest'
import { realmFor, realms, runePaths } from './realms'

const tabs = [
  '/chat', '/automations', '/models', '/train', '/knowledge', '/memory', '/nodes',
  '/performance', '/diagnostics', '/profiles', '/tools', '/api-access', '/settings',
]

describe('realms', () => {
  it('gives every tab a Norse name and a rune that can be drawn', () => {
    for (const tab of tabs) {
      const realm = realms[tab]
      expect(realm, tab).toBeDefined()
      expect(runePaths[realm.rune]).toMatch(/^M/)
    }
    const names = tabs.map((t) => realms[t].norse)
    expect(new Set(names).size).toBe(names.length)
  })

  it('finds the realm for nested routes', () => {
    expect(realmFor('/knowledge/abc')?.norse).toBe('Mimir')
    expect(realmFor('/nowhere')).toBeUndefined()
  })
})
