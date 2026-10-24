// Connect as a device of its own, as the Toskar app does: pair once with
// the code Connect a device shows, then reach the computer at home or,
// with access from anywhere, away from home. Node only (toskar/node).
//
//   node pair.mjs 192.168.1.20:7331 123456   # once, at home
//   node pair.mjs                            # then, from anywhere
import { chmodSync, readFileSync, writeFileSync } from 'node:fs'
import { createInterface } from 'node:readline/promises'

import { pair, Toskar, ToskarError } from 'toskar/node'

const SAVED = 'device.json' // Keep it as a password is kept: it holds the device's key.

if (process.argv.length === 4) {
  const [address, code] = process.argv.slice(2)
  // The computer shows the same short pin next to the code.
  const confirm = async (shortPin) => {
    const ask = createInterface({ input: process.stdin, output: process.stdout })
    const answer = await ask.question(`Does the computer show ${shortPin}? [y/N] `)
    ask.close()
    return answer.trim().toLowerCase() === 'y'
  }
  let device
  try {
    device = await pair(address, code, 'JavaScript example', confirm)
  } catch (err) {
    if (err instanceof ToskarError && err.code === 'NOT_CONFIRMED') {
      console.error("Not paired: the short pin wasn't confirmed.")
      process.exit(1)
    }
    throw err
  }
  writeFileSync(SAVED, JSON.stringify(device))
  chmodSync(SAVED, 0o600)
  console.log('Paired. It shows in Devices on that computer.')
}

const toskar = new Toskar({ device: JSON.parse(readFileSync(SAVED, 'utf8')) })
const version = await toskar.version()
// home, direct, or relay: the way in it found.
console.log(`Reached Toskar ${version.version} ${toskar.path}.`)
