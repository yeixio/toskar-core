import { afterEach, describe, expect, it, vi } from 'vitest'
import { api, downloadArtifact } from './api'

// In the desktop app, the web view can't download or be returned to after a
// sign-in, so these calls go through the shell.

const w = window as unknown as { go?: unknown }

afterEach(() => {
  delete w.go
  vi.unstubAllGlobals()
})

describe('in the desktop app', () => {
  it('saves a chat file through the shell instead of fetching it', async () => {
    const SaveDownload = vi.fn().mockResolvedValue('/Users/me/Downloads/report.csv')
    w.go = { main: { App: { SaveDownload } } }
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
    await downloadArtifact({ id: 'art-1', name: 'report.csv' })
    expect(SaveDownload).toHaveBeenCalledWith('report.csv', '/api/v1/artifacts/art-1/content')
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('asks services to return to the daemon after sign-in', async () => {
    w.go = { main: { App: { GetDaemonURL: vi.fn().mockResolvedValue('http://127.0.0.1:7331') } } }
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ url: 'https://auth.example.com/authorize' }), { status: 200, headers: { 'Content-Type': 'application/json' } }),
    )
    vi.stubGlobal('fetch', fetchMock)
    expect(await api.signInMCPServer('notion')).toBe('https://auth.example.com/authorize')
    const body = JSON.parse(fetchMock.mock.calls[0][1].body as string)
    expect(body.redirect_base).toBe('http://127.0.0.1:7331')
  })
})
