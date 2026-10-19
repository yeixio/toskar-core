import { describe, expect, it } from 'vitest'
import { KNOWN_EVENT_TYPES } from './events'

// The chat page reads the code as text, to find every event type it handles.
const pages = import.meta.glob<string>('../features/chat/ChatPage.tsx', { eager: true, query: '?raw', import: 'default' })

describe('the events the web subscribes to', () => {
  it('include every event the chat page handles (#701, #706)', () => {
    const code = Object.values(pages)[0]
    const handled = [...code.matchAll(/event\.type === '([a-z_.]+)'/g)].map((m) => m[1])
    expect(handled.length).toBeGreaterThan(10)
    const known = new Set<string>(KNOWN_EVENT_TYPES)
    expect(handled.filter((type) => !known.has(type))).toEqual([])
  })
})
