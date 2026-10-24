// Print what happens on Toskar as it happens: models loading, answers
// being written, automations running. Stop with Ctrl-C.
//
//   node events.mjs
import { Toskar } from 'toskar'

const toskar = new Toskar()
for await (const event of toskar.events()) {
  console.log(event.timestamp, event.type, JSON.stringify(event.payload))
}
