import { afterEach, describe, expect, it, vi } from 'vitest'
import { applyTheme } from './uiStore'

afterEach(() => {
  delete (window as unknown as { go?: unknown }).go
})

describe('applyTheme (yeixio/toskar-apps#26)', () => {
  it("gives the desktop window's title bar the page's theme", async () => {
    const SetTheme = vi.fn().mockResolvedValue(undefined)
    ;(window as unknown as { go: unknown }).go = { main: { App: { SetTheme } } }
    applyTheme('light')
    applyTheme('dark')
    await Promise.resolve()
    expect(document.documentElement.dataset.theme).toBe('dark')
    expect(SetTheme.mock.calls).toEqual([['light'], ['dark']])
  })

  it('works in a browser, without the shell', () => {
    expect(() => applyTheme('light')).not.toThrow()
    expect(document.documentElement.dataset.theme).toBe('light')
  })
})
