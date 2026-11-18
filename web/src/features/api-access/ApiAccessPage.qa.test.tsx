import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import { api } from '@/lib/api'
import { ApiAccessPage } from './ApiAccessPage'

// Every other call the page's cards make answers nothing.
vi.mock('@/lib/api', () => {
  const known: Record<string, unknown> = {
    getSettings: vi.fn(),
    listApiKeys: vi.fn(async () => []),
    getNodes: vi.fn(async () => []),
    getApiTLS: vi.fn(async () => ({ enabled: false })),
    getHealth: vi.fn(async () => ({ status: 'ok', product: 'Toskar', version: 'test' })),
    createApiKey: vi.fn(),
  }
  return {
    api: new Proxy(known, { get: (target, name: string) => (target[name] ??= vi.fn(async () => undefined)) }),
    rememberApiKey: vi.fn(),
    forgetApiKey: vi.fn(),
    storedApiKey: vi.fn(() => null),
  }
})

const fresh = {
  lan_api_enabled: false,
  api_host: '127.0.0.1',
  api_port: 7331,
  network_access: { people: 0, devices: 0, keys: 0, sign_in_service: false },
}

function renderPage() {
  return render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <MemoryRouter>
        <ApiAccessPage />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('API Access QA regressions', () => {
  // Regression: toskar-core#1243
  it('a key made in the "Allow access" dialog lets you allow access', async () => {
    vi.mocked(api.getSettings).mockResolvedValue(fresh as never)
    vi.mocked(api.createApiKey).mockImplementation(async (name: string) => {
      // From now on the server counts the key.
      vi.mocked(api.getSettings).mockResolvedValue({ ...fresh, network_access: { ...fresh.network_access, keys: 1 } } as never)
      return { key: { id: 'k1', name, prefix: 'tsk_abc', created_at: '' }, secret: 'tsk_abc_secret' } as never
    })
    renderPage()
    const toggle = await screen.findByRole('switch', { name: 'Local network access' })
    await waitFor(() => expect(toggle).toBeEnabled())
    fireEvent.click(toggle)
    const dialog = await screen.findByRole('dialog', { name: 'Allow access from other computers?' })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Create key' }))
    expect(await within(dialog).findByText('tsk_abc_secret')).toBeInTheDocument()
    await waitFor(() => expect(within(dialog).getByRole('button', { name: 'Allow access' })).toBeEnabled(), { timeout: 2000 })
  })

  // Regression: toskar-core#1254
  it('the API keys section says it is for apps, since devices are listed apart', async () => {
    vi.mocked(api.getSettings).mockResolvedValue(fresh as never)
    renderPage()
    await screen.findByRole('switch', { name: 'Local network access' })
    expect(screen.queryByText(/Credentials for apps and devices/)).not.toBeInTheDocument()
  })

  // Regression: toskar-core#1254
  it("a load error isn't followed by cards full of made-up defaults", async () => {
    vi.mocked(api.getSettings).mockRejectedValue(new Error('offline'))
    renderPage()
    await screen.findByRole('button', { name: /Try again|Retry/ })
    expect(screen.queryByRole('switch', { name: 'Local network access' })).not.toBeInTheDocument()
    expect(screen.queryByText(/0 keys/)).not.toBeInTheDocument()
  })

  // Regression: toskar-core#1254
  it('the LAN copy says HTTPS only when it is on', async () => {
    vi.mocked(api.getSettings).mockResolvedValue({ ...fresh, lan_api_enabled: true } as never)
    renderPage()
    fireEvent.click(await screen.findByRole('button', { name: 'API documentation' }))
    expect(await screen.findByText(/uses plain HTTP/)).toBeInTheDocument()
    expect(screen.queryByText(/uses HTTPS/)).not.toBeInTheDocument()
    expect(document.body.textContent).not.toMatch(/https:\/\/192\.168/)
  })
})
