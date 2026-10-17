import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { api } from '@/lib/api'
import { RemoteAccessCard } from './RemoteAccessCard'

vi.mock('@/lib/api', () => ({
  api: {
    getSettings: vi.fn(),
    getRemoteAccess: vi.fn(),
    updateSettings: vi.fn(),
    checkRemoteAccess: vi.fn(),
    getVersion: vi.fn(),
    startSubscriptionCheckout: vi.fn(),
    manageSubscription: vi.fn(),
  },
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
    expect(screen.getByText(/No relay yet: subscribe above, or in the Toskar app/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Check now' }))
    // The relay works and direct doesn't: reachable, and direct is only a speed-up (#602).
    expect(await screen.findByText('Reachable from outside your network, through the relay')).toBeInTheDocument()
    expect(screen.getByText(/reached in 84 ms/)).toBeInTheDocument()
    expect(screen.getByText('Through relay.toskar.ai')).toBeInTheDocument()
    expect(screen.getByText(/A direct way in is optional/)).toBeInTheDocument()
    expect(screen.getByText('198.51.100.9:7333').closest('li')).toHaveClass('text-ink-muted')
    expect(screen.queryByText(/Devices away from home can't reach this computer/)).not.toBeInTheDocument()
  })

  it('names what answered, shows the route, and keeps "checked moments ago" a note (#602)', async () => {
    vi.mocked(api.getSettings).mockResolvedValue({ remote_access_enabled: true, remote_access_port: 7333 } as never)
    vi.mocked(api.getRemoteAccess).mockResolvedValue({
      enabled: true,
      port: 7333,
      listening: '[::]:7333',
      relay: { name: 'relay.toskar.ai:4443', state: 'connected', route: 'bk2a5ci2' },
      check: {
        at: new Date().toISOString(),
        relay: { ok: true, ms: 562 },
        direct: [
          { address: '198.51.100.9:7333', ok: false, reason: 'wrong_computer', answered: 'Toskar on linix-rig' },
          { address: '[2001:db8::9]:7333', ok: false, reason: 'http', status: 502 },
          { address: '203.0.113.4:7333', ok: false, reason: 'wrong_computer' },
        ],
        error: 'CHECK_TOO_SOON',
      },
    })
    render(
      <QueryClientProvider client={new QueryClient()}>
        <RemoteAccessCard />
      </QueryClientProvider>,
    )
    expect(await screen.findByText(/Toskar on linix-rig answered on this port instead of this computer/)).toBeInTheDocument()
    expect(screen.getByText(/not as expected \(HTTP 502\)/)).toBeInTheDocument()
    expect(screen.getByText(/A web server that isn't Toskar answered/)).toBeInTheDocument()
    expect(screen.getByText("This computer's route on the relay: bk2a5ci2")).toBeInTheDocument()
    expect(screen.getByText(/so this is the latest result/)).toHaveClass('text-ink-muted')
  })

  it('says how the router opened the port, or why it could not, quietly when the relay works (#602)', async () => {
    vi.mocked(api.getSettings).mockResolvedValue({ remote_access_enabled: true, remote_access_port: 7333 } as never)
    vi.mocked(api.getRemoteAccess).mockResolvedValue({
      enabled: true,
      port: 7333,
      listening: '[::]:7333',
      port_mapping: true,
      map_reason: 'port_taken',
      relay: { name: 'relay.toskar.ai:4443', state: 'connected' },
      check: { at: new Date().toISOString(), relay: { ok: true, ms: 90 }, direct: [] },
    })
    render(
      <QueryClientProvider client={new QueryClient()}>
        <RemoteAccessCard />
      </QueryClientProvider>,
    )
    const line = await screen.findByText(/Port 7333 is already forwarded to another device on your router/)
    expect(line).toHaveClass('text-ink-muted')
    // One action a reason: another port, or the relay alone, since it works.
    vi.mocked(api.updateSettings).mockResolvedValue({} as never)
    fireEvent.click(screen.getByRole('button', { name: 'Try another port' }))
    await waitFor(() => expect(api.updateSettings).toHaveBeenCalled())
    const port = (vi.mocked(api.updateSettings).mock.calls.at(-1)?.[0] as { remote_access_port: number }).remote_access_port
    expect(port).toBeGreaterThanOrEqual(20000)
    expect(port).toBeLessThan(40000)
    fireEvent.click(screen.getByRole('button', { name: 'Use the relay only' }))
    await waitFor(() => expect(api.updateSettings).toHaveBeenLastCalledWith({ remote_access_port_mapping: false }))
  })

  it('says the relay alone is in use when port opening is off and the relay works', async () => {
    vi.mocked(api.getSettings).mockResolvedValue({ remote_access_enabled: true, remote_access_port: 7333, remote_access_port_mapping: false } as never)
    vi.mocked(api.getRemoteAccess).mockResolvedValue({
      enabled: true,
      port: 7333,
      listening: '[::]:7333',
      port_mapping: false,
      map_reason: 'off',
      relay: { name: 'relay.toskar.ai:4443', state: 'connected' },
      check: { at: new Date().toISOString(), relay: { ok: true, ms: 90 }, direct: [] },
    })
    render(
      <QueryClientProvider client={new QueryClient()}>
        <RemoteAccessCard />
      </QueryClientProvider>,
    )
    expect(await screen.findByText(/Using the relay only/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Use the relay only' })).not.toBeInTheDocument()
  })

  it('warns about another Toskar running on this computer (#602)', async () => {
    vi.mocked(api.getSettings).mockResolvedValue({ remote_access_enabled: true, remote_access_port: 7333 } as never)
    vi.mocked(api.getRemoteAccess).mockResolvedValue({
      enabled: true,
      port: 7333,
      listening: '[::]:7333',
      others: [{ name: 'linix-rig', pid: 4242, api_port: 7341, data_dir: '/home/mike/.local/share/Toskar', version: '1.8.2', remote: true }],
    })
    render(
      <QueryClientProvider client={new QueryClient()}>
        <RemoteAccessCard />
      </QueryClientProvider>,
    )
    expect(await screen.findByText(/Another Toskar is running on this computer, with access from anywhere on too: linix-rig, version 1\.8\.2, on port 7341/)).toBeInTheDocument()
    expect(screen.getByText(/If you didn't mean to run both, stop the other one/)).toBeInTheDocument()
  })

  it('copies the details for a report, without internet addresses (#602)', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.assign(navigator, { clipboard: { writeText } })
    vi.mocked(api.getVersion).mockResolvedValue({ version: '1.8.3' } as never)
    vi.mocked(api.getSettings).mockResolvedValue({ remote_access_enabled: true, remote_access_port: 7333 } as never)
    vi.mocked(api.getRemoteAccess).mockResolvedValue({
      enabled: true,
      port: 7333,
      listening: '[::]:7333',
      mapped: '198.51.100.9:7333',
      mapped_by: 'upnp',
      relay: { name: 'relay.toskar.ai:4443', state: 'connected', route: 'bk2a5ci2' },
      check: { at: new Date().toISOString(), relay: { ok: true, ms: 90 }, direct: [] },
    })
    render(
      <QueryClientProvider client={new QueryClient()}>
        <RemoteAccessCard />
      </QueryClientProvider>,
    )
    await waitFor(() => expect(api.getVersion).toHaveBeenCalled())
    fireEvent.click(await screen.findByRole('button', { name: 'Copy details for a report' }))
    expect(await screen.findByRole('button', { name: 'Copied' })).toBeInTheDocument()
    const text = writeText.mock.calls[0][0] as string
    expect(text).toContain('Version: 1.8.3')
    expect(text).toContain('<public IPv4>:7333')
    expect(text).not.toContain('198.51.100.9')
  })

  describe("Toskar's relay, bought here (#639)", () => {
    const renderCard = (subscription: object, relay = '') => {
      vi.mocked(api.getSettings).mockResolvedValue({ remote_access_enabled: false, remote_access_port: 7333, remote_access_relay: relay } as never)
      vi.mocked(api.getRemoteAccess).mockResolvedValue({ enabled: false, port: 7333, subscription } as never)
      return render(
        <QueryClientProvider client={new QueryClient()}>
          <RemoteAccessCard />
        </QueryClientProvider>,
      )
    }
    const fakeTab = () => ({ closed: false, opener: {}, location: { href: '' }, close: vi.fn() })

    it('subscribes in a tab opened during the click, with the prices shown', async () => {
      const tab = fakeTab()
      const open = vi.spyOn(window, 'open').mockReturnValue(tab as unknown as Window)
      vi.mocked(api.startSubscriptionCheckout).mockResolvedValue({ url: 'https://checkout.stripe.test/cs_1' })
      renderCard({ state: 'none', open: true })
      expect(await screen.findByText(/Toskar's relay does, for this computer and every device paired with it/)).toBeInTheDocument()
      fireEvent.click(screen.getByRole('button', { name: 'Subscribe for $39.99 a year' }))
      expect(open).toHaveBeenCalledWith('', '_blank')
      expect(tab.opener).toBeNull()
      await waitFor(() => expect(tab.location.href).toBe('https://checkout.stripe.test/cs_1'))
      expect(api.startSubscriptionCheckout).toHaveBeenCalledWith('yearly')
      expect(screen.getByRole('button', { name: 'Subscribe for $4.99 a month' })).toBeInTheDocument()
      open.mockRestore()
    })

    it('closes the tab and says why when the checkout cannot open', async () => {
      const tab = fakeTab()
      const open = vi.spyOn(window, 'open').mockReturnValue(tab as unknown as Window)
      vi.mocked(api.startSubscriptionCheckout).mockRejectedValue(new Error("Billing couldn't be reached. Try again in a minute."))
      renderCard({ state: 'none', open: true })
      fireEvent.click(await screen.findByRole('button', { name: 'Subscribe for $4.99 a month' }))
      expect(await screen.findByText("Billing couldn't be reached. Try again in a minute.")).toBeInTheDocument()
      expect(tab.close).toHaveBeenCalled()
      open.mockRestore()
    })

    it('waits for the payment with a way back to the checkout page', async () => {
      renderCard({ state: 'pending', store: 'stripe', checkout: 'https://checkout.stripe.test/cs_1' })
      expect(await screen.findByText(/Finish paying on the checkout page/)).toBeInTheDocument()
      expect(screen.getByRole('link', { name: 'Open the checkout page again' })).toHaveAttribute('href', 'https://checkout.stripe.test/cs_1')
      expect(screen.queryByRole('button', { name: /Subscribe for/ })).not.toBeInTheDocument()
    })

    it('manages one bought here, and points to the store for one bought in the app', async () => {
      const tab = fakeTab()
      const open = vi.spyOn(window, 'open').mockReturnValue(tab as unknown as Window)
      vi.mocked(api.manageSubscription).mockResolvedValue({ url: 'https://billing.stripe.test/s' })
      const { unmount } = renderCard({ state: 'active', store: 'stripe' })
      fireEvent.click(await screen.findByRole('button', { name: 'Manage billing' }))
      await waitFor(() => expect(tab.location.href).toBe('https://billing.stripe.test/s'))
      unmount()
      open.mockRestore()
      renderCard({ state: 'active', store: 'app' })
      expect(await screen.findByText(/Subscribed in the Toskar app/)).toBeInTheDocument()
      expect(screen.queryByRole('button', { name: 'Manage billing' })).not.toBeInTheDocument()
    })

    it("can't subscribe while the relay is full, and offers it again once one ended", async () => {
      const { unmount } = renderCard({ state: 'none', open: false })
      expect(await screen.findByText(/isn't taking new subscriptions right now/)).toBeInTheDocument()
      expect(screen.getByRole('button', { name: 'Subscribe for $4.99 a month' })).toBeDisabled()
      unmount()
      renderCard({ state: 'ended', store: 'stripe' })
      expect(await screen.findByText(/Your subscription has ended/)).toBeInTheDocument()
      expect(screen.getByRole('button', { name: 'Subscribe for $4.99 a month' })).toBeEnabled()
    })

    it("points to the Toskar app in the Mac App Store's copy, which sells nothing (#630)", async () => {
      renderCard({ state: 'none', in_app_only: true })
      expect(await screen.findByText(/This copy of Toskar is from the App Store, so subscribe in the Toskar app/)).toBeInTheDocument()
      expect(screen.queryByRole('button', { name: /Subscribe for/ })).not.toBeInTheDocument()
    })

    it("isn't shown with an organization's relay", async () => {
      renderCard({ state: 'none' }, 'relay.example.com')
      expect(await screen.findByText('Access from anywhere', { selector: 'h2' })).toBeInTheDocument()
      await waitFor(() => expect(api.getRemoteAccess).toHaveBeenCalled())
      expect(screen.queryByText("Toskar's relay", { selector: 'h3' })).not.toBeInTheDocument()
    })
  })
})
