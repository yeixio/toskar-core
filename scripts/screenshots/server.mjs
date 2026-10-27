// The demo-data server (cmd/screenshot) and loading a screen in it, for
// scripts that capture the web UI.
import { spawn } from 'node:child_process'

/** Starts cmd/screenshot on a free port; resolves to its base URL and a stop function. */
export async function startScreenshotServer(root) {
  const server = spawn('go', ['run', './cmd/screenshot', '-web', 'web/dist'], {
    cwd: root,
    detached: true,
    stdio: ['ignore', 'pipe', 'inherit'],
  })
  const stop = () => {
    try {
      process.kill(-server.pid, 'SIGTERM')
    } catch {
      server.kill()
    }
  }
  let log = ''
  const base = await new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error('screenshot server did not start')), 120_000)
    server.stdout.on('data', (chunk) => {
      log += chunk.toString()
      const match = log.match(/listening (http:\/\/\S+)/)
      if (match) {
        clearTimeout(timer)
        resolve(match[1])
      }
    })
    server.on('exit', (code) => reject(new Error(`screenshot server exited ${code}`)))
  }).catch((error) => {
    stop()
    throw error
  })
  return { base, stop }
}

/** Collects page errors and failed requests, to print when a screen doesn't become ready. */
export function watchPage(page) {
  const log = []
  page.on('pageerror', (error) => log.push(`pageerror: ${error.stack || error.message}`))
  page.on('console', (message) => {
    if (message.type() === 'error' || message.type() === 'warning') log.push(`console.${message.type()}: ${message.text()}`)
  })
  page.on('requestfailed', (request) => log.push(`request failed: ${request.url()} ${request.failure()?.errorText || ''}`))
  page.on('response', (response) => {
    if (response.status() >= 400) log.push(`HTTP ${response.status()}: ${response.url()}`)
  })
  return log
}

/** The screen's URL in screenshot mode, with its query. */
export function screenUrl(base, screen) {
  const query = new URLSearchParams({ screenshot: '1', screen: screen.id })
  for (const [key, value] of new URLSearchParams(screen.query || '')) query.set(key, value)
  return `${base}${screen.path}?${query}`
}

/** Waits until the page is on path and shows text. */
export function waitForText(page, text, pathname, timeout = 30_000) {
  return page.waitForFunction(
    ({ text, pathname }) => (!pathname || location.pathname === pathname) && (document.body?.innerText || '').includes(text),
    { text, pathname },
    { timeout },
  )
}
