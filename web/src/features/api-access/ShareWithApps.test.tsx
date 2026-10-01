import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { api } from '@/lib/api'
import { ShareWithApps } from './ShareWithApps'

vi.mock('@/lib/api', () => ({ api: { mcpShare: vi.fn() } }))

function renderIt() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <ShareWithApps />
    </QueryClientProvider>,
  )
}

describe('ShareWithApps', () => {
  it('gives each app its settings, with the address and key when needed', async () => {
    vi.mocked(api.mcpShare).mockResolvedValue({ url: 'http://127.0.0.1:7444/mcp', command: '/opt/homebrew/bin/yggctl', args: ['mcp'], needs_key: true })
    renderIt()
    const settings = await screen.findByLabelText('json settings')
    expect(settings.textContent).toContain('"command": "/opt/homebrew/bin/yggctl"')
    expect(settings.textContent).toContain('"YGGDRASIL_URL": "http://127.0.0.1:7444"')
    expect(settings.textContent).toContain('"YGGDRASIL_API_KEY": "<your API key>"')
    fireEvent.click(screen.getByRole('tab', { name: 'Claude Code' }))
    expect(screen.getByLabelText('bash settings').textContent).toBe(
      'claude mcp add --transport http yggdrasil http://127.0.0.1:7444/mcp --header "Authorization: Bearer <your API key>"',
    )
    fireEvent.click(screen.getByRole('tab', { name: 'VS Code' }))
    expect(screen.getByLabelText('json settings').textContent).toContain('"type": "http"')
  })

  it('leaves out the address and key on the default local setup', async () => {
    vi.mocked(api.mcpShare).mockResolvedValue({ url: 'http://127.0.0.1:7331/mcp', command: 'yggctl', args: ['mcp'], needs_key: false })
    renderIt()
    const settings = await screen.findByLabelText('json settings')
    expect(settings.textContent).not.toContain('env')
  })
})
