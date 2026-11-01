import { afterEach, describe, expect, it, vi } from 'vitest'
import { streamChat } from './api'

const w = window as unknown as { go?: unknown; runtime?: unknown }

afterEach(() => {
  delete w.go
  delete w.runtime
  vi.unstubAllGlobals()
})

function sse(...events: [string, unknown][]) {
  const text = events.map(([name, data]) => `event: ${name}\ndata: ${JSON.stringify(data)}\n\n`).join('')
  return new Response(text, { status: 200, headers: { 'Content-Type': 'text/event-stream' } })
}

// What a person would see: tokens add to the text, and a replace takes its place.
async function shown(response: Response) {
  const fetchMock = vi.fn().mockResolvedValue(response)
  vi.stubGlobal('fetch', fetchMock)
  let text = ''
  const seen: string[] = []
  await streamChat({
    body: { conversation_id: 'c1', message: 'Hi' },
    onToken: (content) => {
      text += content
      seen.push(text)
    },
    onReplace: (content) => {
      text = content
      seen.push(text)
    },
  })
  return { seen, request: JSON.parse(fetchMock.mock.calls[0][1].body as string) }
}

describe('an answer as it is written (#1006)', () => {
  it('asks for live text', async () => {
    const { request } = await shown(sse(['done', {}]))
    expect(request).toMatchObject({ stream: true, live: true })
  })

  it('takes back narration for a tool call, then shows the answer', async () => {
    const { seen } = await shown(
      sse(['token', { content: 'Let me check.' }], ['replace', { content: '' }], ['token', { content: 'Clean.' }], ['done', {}]),
    )
    expect(seen).toEqual(['Let me check.', '', 'Clean.'])
  })

  it('replaces text a check changed', async () => {
    const { seen } = await shown(sse(['token', { content: 'About 200.' }], ['replace', { content: 'About 230.' }], ['done', {}]))
    expect(seen).toEqual(['About 200.', 'About 230.'])
  })
})
