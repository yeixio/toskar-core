import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/lib/api'
import type { AppNotification } from '@/types/api'
import { NotificationDestinations } from './NotificationDestinations'
import { deliveryNote } from './notificationLabels'

vi.mock('@/lib/api', () => ({
  api: {
    listNotificationDestinations: vi.fn(),
    createNotificationDestination: vi.fn(),
    updateNotificationDestination: vi.fn(),
    deleteNotificationDestination: vi.fn(),
    testNotificationDestination: vi.fn(),
    rotateNotificationSecret: vi.fn(),
    getQuietHours: vi.fn(),
    setQuietHours: vi.fn(),
    listMyDevices: vi.fn(),
  },
}))

function renderCard() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <NotificationDestinations />
    </QueryClientProvider>,
  )
}

describe('Email, push, and webhooks', () => {
  beforeEach(() => {
    vi.mocked(api.listNotificationDestinations).mockResolvedValue([])
    vi.mocked(api.getQuietHours).mockResolvedValue({ enabled: false, start: '22:00', end: '07:00', time_zone: 'UTC', allow: 'errors' })
  })

  it('adds the Toskar app, sending the title only unless asked, and says when no device has notifications on (#460)', async () => {
    vi.mocked(api.listMyDevices).mockResolvedValue([
      { id: 'k1', name: 'Phone', prefix: 'p', created_at: '', revoked: false, permissions: {}, person_id: 'owner', push: false },
    ] as never)
    vi.mocked(api.createNotificationDestination).mockResolvedValue({
      destination: { id: 'd2', kind: 'app', name: 'My phone and tablet', enabled: true, has_secret: false, app: { content: 'title' } },
    } as never)
    renderCard()
    fireEvent.click(await screen.findByRole('button', { name: 'Add the Toskar app' }))
    expect(await screen.findByText(/None of your devices has notifications on yet/)).toBeInTheDocument()
    expect(screen.getByLabelText('What to send')).toHaveValue('title')
    fireEvent.click(screen.getByRole('button', { name: /^Save|^Add/ }))
    await waitFor(() =>
      expect(api.createNotificationDestination).toHaveBeenCalledWith(expect.objectContaining({ kind: 'app', app: { content: 'title' } })),
    )
  })

  it('adds a webhook and shows its signing secret once', async () => {
    vi.mocked(api.createNotificationDestination).mockResolvedValue({
      destination: { id: 'd1', kind: 'webhook', name: 'Home server', enabled: true, has_secret: true, webhook: { url: 'https://h.example/y' } },
      secret: 'whsec_abc123',
    })
    renderCard()
    fireEvent.click(await screen.findByRole('button', { name: 'Add webhook' }))
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Home server' } })
    fireEvent.change(screen.getByLabelText('Address'), { target: { value: 'https://h.example/y' } })
    fireEvent.click(screen.getByLabelText('Automations'))
    fireEvent.change(screen.getByLabelText('Send'), { target: { value: 'error' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add' }))
    expect(await screen.findByText('whsec_abc123')).toBeInTheDocument()
    expect(api.createNotificationDestination).toHaveBeenCalledWith({
      kind: 'webhook',
      name: 'Home server',
      webhook: { url: 'https://h.example/y' },
      categories: ['automation'],
      min_severity: 'error',
    })
  })

  it('sends a daily digest instead of each notice', async () => {
    vi.mocked(api.createNotificationDestination).mockResolvedValue({
      destination: { id: 'd2', kind: 'webhook', name: 'Digest', enabled: true, has_secret: true, webhook: { url: 'https://h.example/d' }, digest: { at: '07:30', time_zone: 'UTC' } },
      secret: 'whsec_def',
    })
    renderCard()
    fireEvent.click(await screen.findByRole('button', { name: 'Add webhook' }))
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Digest' } })
    fireEvent.change(screen.getByLabelText('Address'), { target: { value: 'https://h.example/d' } })
    expect(screen.queryByLabelText('Send the digest at')).not.toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('When'), { target: { value: 'digest' } })
    expect(screen.getByText(/Errors still go out right away/)).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('Send the digest at'), { target: { value: '07:30' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add' }))
    await waitFor(() => expect(api.createNotificationDestination).toHaveBeenCalled())
    expect(vi.mocked(api.createNotificationDestination).mock.calls[0][0]).toMatchObject({
      digest: { at: '07:30', time_zone: Intl.DateTimeFormat().resolvedOptions().timeZone },
    })
  })

  it('adds push through ntfy with a random topic', async () => {
    vi.mocked(api.createNotificationDestination).mockResolvedValue({
      destination: { id: 'n1', kind: 'ntfy', name: 'My phone', enabled: true, has_secret: false, ntfy: { server: 'https://ntfy.sh', topic: 't' } },
    })
    renderCard()
    fireEvent.click(await screen.findByRole('button', { name: 'Add push (ntfy)' }))
    expect((screen.getByLabelText('Topic') as HTMLInputElement).value).toMatch(/^yggdrasil-[0-9a-f]{16}$/)
    fireEvent.change(screen.getByLabelText('Topic'), { target: { value: 'my-topic' } })
    fireEvent.change(screen.getByLabelText('What to send'), { target: { value: 'private' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add' }))
    await waitFor(() => expect(api.createNotificationDestination).toHaveBeenCalled())
    expect(vi.mocked(api.createNotificationDestination).mock.calls[0][0]).toMatchObject({
      kind: 'ntfy',
      name: 'My phone',
      ntfy: { server: 'https://ntfy.sh', topic: 'my-topic', content: 'private' },
    })
  })

  it('tests a destination and shows why it failed', async () => {
    vi.mocked(api.listNotificationDestinations).mockResolvedValue([
      {
        id: 'e1',
        kind: 'email',
        name: 'Me',
        enabled: true,
        has_secret: true,
        email: { host: 'smtp.example.com', port: 587, from: 'a@example.com', to: ['me@example.com'] },
      },
    ])
    vi.mocked(api.testNotificationDestination).mockResolvedValue({
      ok: false,
      error: 'the SMTP server did not accept the username and password (535)',
      permanent: true,
    })
    renderCard()
    expect(await screen.findByText(/me@example.com · All categories · Everything/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Test' }))
    expect(await screen.findByText(/did not accept the username and password/)).toBeInTheDocument()
  })

  it('turns on quiet hours in the local time zone', async () => {
    vi.mocked(api.setQuietHours).mockImplementation(async (q) => q)
    renderCard()
    fireEvent.click(await screen.findByLabelText('Quiet hours'))
    await waitFor(() => expect(api.setQuietHours).toHaveBeenCalled())
    const saved = vi.mocked(api.setQuietHours).mock.calls[0][0]
    expect(saved.enabled).toBe(true)
    expect(saved.time_zone).toBe(Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC')
  })
})

describe('deliveryNote', () => {
  const n = (deliveries: AppNotification['deliveries']): AppNotification => ({
    id: 'n',
    created_at: '2026-10-02T00:00:00Z',
    source_type: 'automation',
    category: 'automation',
    severity: 'info',
    title: 't',
    body: '',
    deliveries,
  })
  it('says what happened outside the app', () => {
    expect(deliveryNote(n([{ channel: 'desktop', status: 'delivered', attempts: 1 }]))).toBe('')
    expect(deliveryNote(n([{ channel: 'email:1', status: 'held', attempts: 0 }]))).toBe('held for quiet hours')
    expect(deliveryNote(n([{ channel: 'webhook:1', status: 'pending', attempts: 2 }]))).toBe('retrying delivery')
    expect(deliveryNote(n([{ channel: 'webhook:1', status: 'failed', attempts: 4 }]))).toBe('not delivered everywhere')
  })
})
