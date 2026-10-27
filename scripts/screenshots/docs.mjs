// Captures the user guide's screenshots (docs/user-guide/screens.json) from
// the web UI with demo data, into docs/user-guide/images/<id>.png. A guide
// image names its screen, so the screenshots follow the interface instead of
// being taken by hand (yeixio/toskar-core#882).
//
//   node scripts/screenshots/docs.mjs              every screen
//   DOCS_SCREENS=chat,models node ...docs.mjs      only these
import { mkdir, readFile } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { chromium } from 'playwright'
import { screenUrl, startScreenshotServer, waitForText, watchPage } from './server.mjs'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..')
const config = JSON.parse(await readFile(path.join(root, 'docs/user-guide/screens.json'), 'utf8'))
const wanted = (process.env.DOCS_SCREENS || '').split(',').map((id) => id.trim()).filter(Boolean)
const unknown = wanted.filter((id) => !config.screens.some((s) => s.id === id))
if (unknown.length) throw new Error(`No such screen in docs/user-guide/screens.json: ${unknown.join(', ')}`)
const screens = config.screens.filter((s) => !wanted.length || wanted.includes(s.id))
const outDir = path.join(root, 'docs/user-guide/images')
await mkdir(outDir, { recursive: true })

// An action reaches the state a page describes: a dialog open, a field
// filled in. Buttons and links are found by their visible name, as a reader
// would find them.
async function act(page, action) {
  if (action.click) {
    const byRole = page.getByRole('button', { name: action.click }).or(page.getByRole('link', { name: action.click })).or(page.getByRole('tab', { name: action.click }))
    const target = (await byRole.count()) ? byRole.first() : page.getByText(action.click, { exact: false }).first()
    await target.click()
  } else if (action.fill) {
    await page.getByLabel(action.fill.label).first().fill(action.fill.text)
  } else if (action.wait) {
    await waitForText(page, action.wait)
  } else {
    throw new Error(`Unknown action ${JSON.stringify(action)}`)
  }
}

async function capture(page, screen) {
  await page.goto(screenUrl(server.base, screen), { waitUntil: 'domcontentloaded' })
  await waitForText(page, screen.readyText, screen.path)
  for (const action of screen.actions || []) await act(page, action)
  // Fonts, images, and transitions settle before the picture is taken.
  await page.evaluate(() => document.fonts?.ready)
  await page.waitForTimeout(300)
  await page.screenshot({ path: path.join(outDir, `${screen.id}.png`) })
}

const server = await startScreenshotServer(root)
try {
  const browser = await chromium.launch(process.env.SCREENSHOT_CHROME ? { executablePath: process.env.SCREENSHOT_CHROME } : {})
  for (const screen of screens) {
    const context = await browser.newContext({
      viewport: screen.viewport || config.viewport,
      deviceScaleFactor: screen.deviceScaleFactor || config.deviceScaleFactor || 1,
      colorScheme: screen.theme || config.theme || 'dark',
    })
    if (screen.form) await context.addInitScript((name) => { document.documentElement.dataset.form = name }, screen.form)
    const page = await context.newPage()
    const log = watchPage(page)
    console.log(`Capturing ${screen.id}`)
    try {
      await capture(page, screen)
    } catch {
      // On CI a screen occasionally stays blank; it gets one more try.
      console.error(`Loading ${screen.id} again`)
      try {
        await capture(page, screen)
      } catch (error) {
        const text = await page.locator('body').innerText({ timeout: 5_000 }).catch(() => '(could not read the page)')
        console.error(`Page text for ${screen.id}:\n${text.slice(0, 1200)}`)
        console.error(`Page errors for ${screen.id}:\n${log.slice(-40).join('\n') || '(none)'}`)
        await page.screenshot({ path: path.join(root, `screenshot-failed-${screen.id}.png`) }).catch(() => {})
        throw error
      }
    }
    await context.close()
  }
  await browser.close()
  console.log(`User guide screenshots: ${path.relative(root, outDir)} (${screens.length})`)
} finally {
  server.stop()
}
