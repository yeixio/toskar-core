import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  openExternal,
  preopenSignInWindow,
  routeExternalLinks,
  saveFromDaemon,
  saveText,
  signInReturnAddress,
} from './desktopBridge'

type Shell = {
  BrowserOpenURL: ReturnType<typeof vi.fn>
  SaveDownload: ReturnType<typeof vi.fn>
  SaveData: ReturnType<typeof vi.fn>
  GetDaemonURL: ReturnType<typeof vi.fn>
}

/** Pretends the page runs inside the Wails desktop shell. */
function inShell(): Shell {
  const shell = {
    BrowserOpenURL: vi.fn(),
    SaveDownload: vi.fn().mockResolvedValue('/Users/me/Downloads/report.csv'),
    SaveData: vi.fn().mockResolvedValue('/Users/me/Downloads/inventory.csv'),
    GetDaemonURL: vi.fn().mockResolvedValue('http://127.0.0.1:7331'),
  }
  const w = window as unknown as Record<string, unknown>
  w.go = { main: { App: { SaveDownload: shell.SaveDownload, SaveData: shell.SaveData, GetDaemonURL: shell.GetDaemonURL } } }
  w.runtime = { BrowserOpenURL: shell.BrowserOpenURL }
  return shell
}

function link(href: string, attrs: Record<string, string> = {}): HTMLAnchorElement {
  const a = document.createElement('a')
  a.href = href
  for (const [k, v] of Object.entries(attrs)) a.setAttribute(k, v)
  a.textContent = 'link'
  document.body.appendChild(a)
  return a
}

/** Clicks el and reports whether the shell's handler took the click. */
function click(el: Element): { defaultPrevented: boolean } {
  let taken = false
  // Runs after the capture-phase handler; then stops jsdom following the link.
  const settle = (e: Event) => {
    taken = e.defaultPrevented
    e.preventDefault()
  }
  document.addEventListener('click', settle, { once: true })
  el.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true, button: 0 }))
  return { defaultPrevented: taken }
}

afterEach(() => {
  const w = window as unknown as Record<string, unknown>
  delete w.go
  delete w.runtime
  document.body.innerHTML = ''
  vi.restoreAllMocks()
})

describe('external links', () => {
  it('opens links that leave the app in the browser, from the desktop app', () => {
    const shell = inShell()
    const stop = routeExternalLinks()
    const source = link('https://www.michelin.com/tires', { target: '_blank' })
    const inline = link('https://example.com/a')
    const mail = link('mailto:help@example.com')

    expect(click(source).defaultPrevented).toBe(true)
    expect(click(inline).defaultPrevented).toBe(true)
    expect(click(mail).defaultPrevented).toBe(true)
    expect(shell.BrowserOpenURL.mock.calls.map((c) => c[0])).toEqual([
      'https://www.michelin.com/tires',
      'https://example.com/a',
      'mailto:help@example.com',
    ])
    stop()
    click(source)
    expect(shell.BrowserOpenURL).toHaveBeenCalledTimes(3)
  })

  it('leaves the app’s own links and downloads alone', () => {
    const shell = inShell()
    const stop = routeExternalLinks()
    const own = link(`${window.location.origin}/knowledge`, { target: '_blank' })
    const file = link('https://example.com/file.csv', { download: 'file.csv' })
    const inner = document.createElement('span')
    link('https://example.com/b').appendChild(inner)

    expect(click(own).defaultPrevented).toBe(false)
    expect(click(file).defaultPrevented).toBe(false)
    expect(click(inner).defaultPrevented).toBe(true) // a click inside a link counts
    expect(shell.BrowserOpenURL).toHaveBeenCalledTimes(1)
    stop()
  })

  it('does nothing in a normal browser', () => {
    const stop = routeExternalLinks()
    expect(click(link('https://example.com', { target: '_blank' })).defaultPrevented).toBe(false)
    stop()
  })

  it('openExternal uses the shell, or a new tab in a browser', () => {
    const open = vi.spyOn(window, 'open').mockReturnValue(null)
    openExternal('https://example.com')
    expect(open).toHaveBeenCalledWith('https://example.com', '_blank', 'noopener,noreferrer')

    const shell = inShell()
    openExternal('https://example.com/2')
    expect(shell.BrowserOpenURL).toHaveBeenCalledWith('https://example.com/2')
    expect(open).toHaveBeenCalledTimes(1)
  })
})

describe('sign-in', () => {
  it('returns to the daemon’s address from the desktop app', async () => {
    expect(await signInReturnAddress()).toBe(window.location.origin)
    inShell()
    expect(await signInReturnAddress()).toBe('http://127.0.0.1:7331')
  })

  it('opens no placeholder window in the desktop app', () => {
    const open = vi.spyOn(window, 'open').mockReturnValue(null)
    preopenSignInWindow()
    expect(open).toHaveBeenCalledTimes(1)
    inShell()
    expect(preopenSignInWindow()).toBeNull()
    expect(open).toHaveBeenCalledTimes(1)
  })
})

describe('saving files', () => {
  it('returns null outside the desktop app, so the caller downloads as usual', async () => {
    expect(await saveFromDaemon('a.txt', '/api/v1/artifacts/a/content')).toBeNull()
    expect(await saveText('a.txt', 'hi')).toBeNull()
  })

  it('asks the shell to save a daemon file', async () => {
    const shell = inShell()
    expect(await saveFromDaemon('report.csv', '/api/v1/artifacts/r1/content')).toBe('/Users/me/Downloads/report.csv')
    expect(shell.SaveDownload).toHaveBeenCalledWith('report.csv', '/api/v1/artifacts/r1/content')
  })

  it('sends text to the shell as UTF-8 base64', async () => {
    const shell = inShell()
    await saveText('notes.txt', 'Níðhöggr ✓')
    const sent = shell.SaveData.mock.calls[0][1] as string
    const bytes = Uint8Array.from(atob(sent), (c) => c.charCodeAt(0))
    expect(new TextDecoder().decode(bytes)).toBe('Níðhöggr ✓')
  })
})
