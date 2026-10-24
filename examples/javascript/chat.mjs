// Stream an answer, a piece at a time, with the JavaScript SDK.
//
//   node chat.mjs "Plan a three-day trip to Juneau"
import { ErrorCodes, Toskar, ToskarError } from 'toskar'

const message = process.argv.slice(2).join(' ') || 'Hello'

// This computer's Toskar Core needs no key. Elsewhere, set TOSKAR_URL and
// TOSKAR_API_KEY, or pass baseUrl and apiKey.
const toskar = new Toskar()

try {
  for await (const event of toskar.chatStream({ message })) {
    if (event.type === 'token') process.stdout.write(event.content)
    if (event.type === 'done' && event.metrics) console.log(`\n\n${event.metrics.completion_tokens} tokens`)
  }
} catch (err) {
  if (err instanceof ToskarError && err.code === ErrorCodes.NO_MODEL_INSTALLED) {
    console.error('Install a model first: open Toskar and choose one in Models.')
    process.exit(1)
  }
  throw err
}
