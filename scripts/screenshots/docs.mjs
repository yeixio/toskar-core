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
    // exact: the whole name, so Install isn't the Installed tab.
    const named = { name: action.click, exact: Boolean(action.exact) }
    const byRole = page.getByRole('button', named).or(page.getByRole('link', named)).or(page.getByRole('tab', named))
    const target = (await byRole.count()) ? byRole.first() : page.getByText(action.click, { exact: Boolean(action.exact) }).first()
    await target.click()
  } else if (action.fill) {
    await page.getByLabel(action.fill.label).first().fill(action.fill.text)
  } else if (action.select) {
    // A menu's name starts with its label and goes on with its choice, such
    // as "Model Gemma 4 E4B", so it's found by how it starts.
    const starts = new RegExp('^' + action.select.label.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'))
    await page.getByRole('combobox', { name: starts }).first().selectOption({ label: action.select.option })
  } else if (action.scroll) {
    // Brings part of a long form into view, such as how an automation runs.
    const exact = page.getByText(action.scroll, { exact: true })
    const target = (await exact.count()) ? exact.first() : page.getByText(action.scroll).first()
    await target.evaluate((el) => el.scrollIntoView({ block: 'center' }))
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
    // api: an answer in place of the demo data's, for a state the demo
    // doesn't have, such as a Toskar Pro trial.
    // "{{in:600}}" in an answer is the time 600 seconds from the capture,
    // for something that counts down, such as a pairing code's expiry.
    const timed = (value) =>
      typeof value === 'string'
        ? value.replace(/\{\{in:(-?\d+)\}\}/g, (_, s) => new Date(Date.now() + Number(s) * 1000).toISOString())
        : Array.isArray(value)
          ? value.map(timed)
          : value && typeof value === 'object'
            ? Object.fromEntries(Object.entries(value).map(([k, v]) => [k, timed(v)]))
            : value
    for (const [apiPath, body] of Object.entries(screen.api || {})) {
      await context.route((url) => url.pathname === apiPath, (route) => route.fulfill({ json: timed(body) }))
    }
    // desktopApp: the page as the desktop app shows it, with what the app
    // gives it (desktop/app.go's bindings), such as the store's price.
    if (screen.desktopApp) {
      await context.addInitScript((app) => {
        const outcome = (value) => async () => value
        window.go = { main: { App: {
          ProUnlockPrice: outcome(app.unlockPrice || ''),
          ProUnlock: outcome('cancelled'),
          ProRestore: outcome('none'),
          ...(app.startTrial ? { ProStartTrial: outcome('cancelled') } : {}),
        } } }
      }, screen.desktopApp)
    }
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
