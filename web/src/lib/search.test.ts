import { describe, expect, it } from 'vitest'

import { matchesSearch, searchItems, searchMatch } from './search'

describe('search with typos (#451)', () => {
  it('finds what it found before, exactly', () => {
    expect(searchMatch('llama', 'Llama 3.2 1B')).toBe('exact')
    expect(searchMatch('lama', 'Llama 3.2 1B')).toBe('exact') // "lama" is in "llama"
    expect(searchMatch('3.2', 'Llama 3.2 1B')).toBe('exact')
    expect(searchMatch('', 'anything')).toBe('exact')
  })

  it('finds a near miss', () => {
    expect(searchMatch('mistrl', 'Mistral 7B')).toBe('near')
    expect(searchMatch('atomations', 'Automations')).toBe('near')
    expect(searchMatch('qwen corder', 'Qwen 2.5 Coder 7B')).toBe('near')
    // As it's typed: a typo in the start of a longer word.
    expect(searchMatch('atomat', 'Automations')).toBe('near')
  })

  it('keeps short searches exact, and misses stay misses', () => {
    expect(matchesSearch('lam', 'Llama')).toBe(true)
    expect(matchesSearch('lma', 'Gemma')).toBe(false)
    expect(matchesSearch('phi', 'Mistral')).toBe(false)
    expect(matchesSearch('weather report', 'Stock check')).toBe(false)
  })

  it('needs every word of the search', () => {
    expect(matchesSearch('mistrl coder', 'Mistral 7B')).toBe(false)
  })

  it('lists exact matches first, each group in order', () => {
    const items = ['Gemma 2 9B', 'Gema notes', 'Llama 3.2 1B', 'Gemma 3 4B']
    expect(searchItems(items, 'gema', (s) => [s])).toEqual(['Gema notes', 'Gemma 2 9B', 'Gemma 3 4B'])
    expect(searchItems(items, 'gemma', (s) => [s])).toEqual(['Gemma 2 9B', 'Gemma 3 4B', 'Gema notes'])
    expect(searchItems(items, '', (s) => [s])).toBe(items)
  })
})
