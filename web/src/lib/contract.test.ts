import { afterEach, describe, expect, it, vi } from 'vitest'
import { api, CLIENT_CONTRACT, CLIENT_CONTRACT_HEADER } from './api'

describe('client contract', () => {
  afterEach(() => vi.restoreAllMocks())

  it('sends the contract the app is built for, and shows a mismatch plainly', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ error: { code: 'CONTRACT_MISMATCH', message: 'this app needs Yggdrasil contract 2.x. Update Yggdrasil' } }), {
        status: 426,
        headers: { 'Content-Type': 'application/json', 'Yggdrasil-Contract': '1.0' },
      }),
    )
    await expect(api.getRun('r1')).rejects.toThrow(/Update Yggdrasil/)
    const headers = fetchMock.mock.calls[0][1]?.headers as Record<string, string>
    expect(headers[CLIENT_CONTRACT_HEADER]).toBe(CLIENT_CONTRACT)
  })
})
