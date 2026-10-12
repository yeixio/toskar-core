import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { api } from '@/lib/api'
import { RemoteAccessCard } from './RemoteAccessCard'

vi.mock('@/lib/api', () => ({
  api: { getSettings: vi.fn(), getRemoteAccess: vi.fn(), updateSettings: vi.fn(), checkRemoteAccess: vi.fn() },
}))

describe('RemoteAccessCard (#456)', () => {
  it('turns access from anywhere on, shows the listener, and saves a forwarded address', async () => {
    vi.mocked(api.getSettings).mockResolvedValue({ remote_access_enabled: true, remote_access_port: 7333 } as never)
    vi.mocked(api.getRemoteAccess).mockResolvedValue({ enabled: true, port: 7333, listening: '[::]:7333', reachable: 'direct', mapped: '203.0.113.9:7333', mapped_by: 'upnp' })
    vi.mocked(api.updateSettings).mockResolvedValue({} as never)
    render(
      <QueryClientProvider client={new QueryClient()}>
        <RemoteAccessCard />
      </QueryClientProvider>,
    )
    expect(await screen.findByText('Reachable from anywhere at 203.0.113.9:7333. Your router opened the port (UPnP).')).toBeInTheDocument()
    fireEvent.click(screen.getByLabelText('Open a port on the router automatically'))
    await waitFor(() => expect(api.updateSettings).toHaveBeenCalledWith({ remote_access_port_mapping: false }))
    fireEvent.change(screen.getByLabelText('Forwarded address (optional)'), { target: { value: ' home.example.com ' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(api.updateSettings).toHaveBeenCalledWith({ remote_access_port: 7333, remote_access_address: 'home.example.com' }))
    fireEvent.click(screen.getByRole('switch', { name: 'Access from anywhere' }))
    await waitFor(() => expect(api.updateSettings).toHaveBeenCalledWith({ remote_access_enabled: false }))
  })

  it("connects to an organization's relay, keeps the secret out of sight, and says how it's going", async () => {
    vi.mocked(api.getSettings).mockResolvedValue({
      remote_access_enabled: true,
      remote_access_port: 7333,
      remote_access_relay: 'relay.example.com',
      remote_access_relay_enrolled: true,
    } as never)
    vi.mocked(api.getRemoteAccess).mockResolvedValue({
      enabled: true,
      port: 7333,
      listening: '[::]:7333',
      reachable: 'relay',
      relay: { name: 'relay.example.com', state: 'connected' },
    })
    vi.mocked(api.updateSettings).mockClear().mockResolvedValue({} as never)
    render(
      <QueryClientProvider client={new QueryClient()}>
        <RemoteAccessCard />
      </QueryClientProvider>,
    )
    expect(await screen.findByText('Reachable from anywhere through relay.example.com.')).toBeInTheDocument()
    expect(screen.getByText('Connected to relay.example.com.')).toBeInTheDocument()
    const secret = screen.getByLabelText('Enrollment secret')
    expect(secret).toHaveValue('')
    expect(secret).toHaveAttribute('placeholder', 'Saved. Paste a new one to replace it.')
    fireEvent.change(secret, { target: { value: ' new-secret-0123456789abcdef ' } })
    fireEvent.click(screen.getByRole('button', { name: 'Connect' }))
    await waitFor(() => expect(api.updateSettings).toHaveBeenCalledWith({ remote_access_relay_secret: 'new-secret-0123456789abcdef' }))
    expect(secret).toHaveValue('')
    fireEvent.click(screen.getByRole('button', { name: "Use Toskar's relay instead" }))
    await waitFor(() => expect(api.updateSettings).toHaveBeenCalledWith({ remote_access_relay: '', remote_access_relay_secret: '' }))
  })

  it('shows the outside check: what reached this computer, what failed and why, and Check now (#546)', async () => {
    const at = new Date(Date.now() - 5 * 60_000).toISOString()
    vi.mocked(api.getSettings).mockResolvedValue({ remote_access_enabled: true, remote_access_port: 7333 } as never)
    vi.mocked(api.getRemoteAccess).mockResolvedValue({
      enabled: true,
      port: 7333,
      listening: '[::]:7333',
      reachable: 'direct',
      mapped: '198.51.100.9:7333',
      mapped_by: 'upnp',
      relay: { name: 'relay.toskar.ai:4443', state: 'no_token' },
      check: { at, relay: { ok: false, reason: 'no_tunnel' }, direct: [{ address: '198.51.100.9:7333', ok: false, reason: 'timeout', ms: 8000 }] },
    })
    vi.mocked(api.checkRemoteAccess).mockResolvedValue({
      enabled: true,
      port: 7333,
      listening: '[::]:7333',
      relay: { name: 'relay.toskar.ai:4443', state: 'connected' },
      check: { at: new Date().toISOString(), relay: { ok: true, ms: 84 }, direct: [{ address: '198.51.100.9:7333', ok: false, reason: 'timeout' }] },
    })
    render(
      <QueryClientProvider client={new QueryClient()}>
        <RemoteAccessCard />
      </QueryClientProvider>,
    )
    // The router said it opened the port, but nothing reached it from outside.
    expect(await screen.findByText('Not reachable from outside your network')).toBeInTheDocument()
    expect(screen.getByText('Checked 5 minutes ago')).toBeInTheDocument()
    expect(screen.getByText(/Another router in front of yours \(double NAT\)/)).toBeInTheDocument()
    expect(screen.getByText(/subscribe to access from anywhere in the Toskar app/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Check now' }))
    expect(await screen.findByText('Reachable from outside your network')).toBeInTheDocument()
    expect(screen.getByText(/reached in 84 ms/)).toBeInTheDocument()
    expect(screen.getByText('Through relay.toskar.ai')).toBeInTheDocument()
  })
})
