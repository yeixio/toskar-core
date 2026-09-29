import { spawn } from 'node:child_process'
import { mkdir, readFile } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { chromium } from 'playwright'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..')
const config = JSON.parse(await readFile(path.join(root, 'screenshots/config.json'), 'utf8'))
const wanted = (process.env.SCREENSHOT_FORMS || '')
  .split(',')
  .map((id) => id.trim())
  .filter(Boolean)
const forms = (config.forms?.length
  ? config.forms
  : [
      {
        id: 'desktop',
        readme: true,
        viewport: {
          width: config.window?.width || 1440,
          height: config.window?.height || 900,
        },
        deviceScaleFactor: 1,
      },
    ]
).filter((form) => wanted.length === 0 || wanted.includes(form.id))
const readmeDir = path.join(root, 'docs/screenshots')
await mkdir(readmeDir, { recursive: true })

const server = spawn('go', ['run', './cmd/screenshot', '-web', 'web/dist'], {
  cwd: root,
  detached: true,
  stdio: ['ignore', 'pipe', 'inherit'],
})
let base = ''
let log = ''
const ready = new Promise((resolve, reject) => {
  const timer = setTimeout(() => reject(new Error('screenshot server did not start')), 120_000)
  server.stdout.on('data', (chunk) => {
    log += chunk.toString()
    const match = log.match(/listening (http:\/\/\S+)/)
    if (match) {
      clearTimeout(timer)
      resolve(match[1])
    }
  })
  server.on('exit', (code) => {
    if (!base) reject(new Error(`screenshot server exited ${code}`))
  })
})

async function openScreen(page, screen) {
  const query = new URLSearchParams({ screenshot: '1', screen: screen.id })
  if (screen.query) {
    for (const [key, value] of new URLSearchParams(screen.query)) {
      query.set(key, value)
    }
  }
  const url = `${base}${screen.path}?${query}`
  await page.goto(url, { waitUntil: 'domcontentloaded' })
  try {
    await page.waitForFunction(
      ({ text, path }) => location.pathname === path && (document.body?.innerText || '').includes(text),
      { text: screen.readyText, path: screen.path },
      { timeout: 30_000 },
    )
  } catch (error) {
    const text = await page.locator('body').innerText().catch(() => '')
    console.error(`Page text for ${screen.id}:\n${text.slice(0, 1200)}`)
    throw error
  }
}

try {
  base = await ready
  const browser = await chromium.launch(
    process.env.SCREENSHOT_CHROME ? { executablePath: process.env.SCREENSHOT_CHROME } : {},
  )
  for (const form of forms) {
    const viewport = form.viewport || { width: 1440, height: 900 }
    const context = await browser.newContext({
      viewport,
      deviceScaleFactor: form.deviceScaleFactor || 1,
    })
    const layout = form.layout || (form.hideSidebar ? 'phone' : '')
    await context.addInitScript((name) => {
      if (name) document.documentElement.dataset.form = name
    }, layout)
    const page = await context.newPage()
    const outDir = form.readme ? readmeDir : path.join(root, 'screenshots/raw', form.id)
    await mkdir(outDir, { recursive: true })
    for (const screen of config.screens) {
      console.log(`Capturing ${form.id} ${screen.id}`)
      await openScreen(page, screen)
      await page.evaluate((name) => {
        if (name) document.documentElement.dataset.form = name
        else delete document.documentElement.dataset.form
        if (name !== 'phone') return
        const transcript = document.querySelector('.chat-transcript')
        if (transcript) transcript.scrollTop = 0
      }, layout)
      await page.screenshot({ path: path.join(outDir, screen.filename) })
    }
    await context.close()
    console.log(`Screenshots: ${outDir}`)
  }
  await browser.close()
} finally {
  try {
    process.kill(-server.pid, 'SIGTERM')
  } catch {
    server.kill()
  }
}
