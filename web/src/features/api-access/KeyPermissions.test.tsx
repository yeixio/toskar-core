import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { api } from '@/lib/api'
import { KeyPermissions } from './KeyPermissions'

vi.mock('@/lib/api', () => ({
  api: {
    setApiKeyPermissions: vi.fn(),
    getProfiles: vi.fn().mockResolvedValue([{ id: 'tires', name: 'Tire Shop' }]),
  },
}))

describe('KeyPermissions', () => {
  it('changes one permission and keeps the rest', async () => {
    vi.mocked(api.setApiKeyPermissions).mockResolvedValue(null)
    render(
      <QueryClientProvider client={new QueryClient()}>
        <KeyPermissions
          apiKey={{ id: 'k1', name: 'bot', prefix: 'ygg_abc', created_at: '2026-10-01T00:00:00Z', revoked: false }}
        />
      </QueryClientProvider>,
    )
    expect(screen.getByLabelText('Your memories')).toHaveValue('on_request')
    fireEvent.change(screen.getByLabelText('Tools'), { target: { value: 'read_only' } })
    await waitFor(() =>
      expect(api.setApiKeyPermissions).toHaveBeenCalledWith('k1', {
        memory: 'on_request',
        knowledge: 'always',
        tools: 'read_only',
        placement: true,
      }),
    )
  })

  it('pins the key to a profile', async () => {
    vi.mocked(api.setApiKeyPermissions).mockResolvedValue(null)
    render(
      <QueryClientProvider client={new QueryClient()}>
        <KeyPermissions
          apiKey={{ id: 'k2', name: 'site', prefix: 'ygg_def', created_at: '2026-10-01T00:00:00Z', revoked: false }}
        />
      </QueryClientProvider>,
    )
    await screen.findByRole('option', { name: 'Tire Shop' })
    fireEvent.change(screen.getByLabelText('Answers with'), { target: { value: 'tires' } })
    await waitFor(() =>
      expect(api.setApiKeyPermissions).toHaveBeenCalledWith('k2', {
        memory: 'on_request',
        knowledge: 'always',
        tools: 'profile',
        placement: true,
        profile: 'tires',
      }),
    )
  })

  it('makes a key that only asks, as a CI job needs', async () => {
    vi.mocked(api.setApiKeyPermissions).mockResolvedValue(null)
    render(
      <QueryClientProvider client={new QueryClient()}>
        <KeyPermissions
          apiKey={{ id: 'k3', name: 'ci', prefix: 'ygg_ghi', created_at: '2026-10-01T00:00:00Z', revoked: false }}
        />
      </QueryClientProvider>,
    )
    fireEvent.change(screen.getByLabelText('Reaches'), { target: { value: 'chat' } })
    await waitFor(() =>
      expect(api.setApiKeyPermissions).toHaveBeenCalledWith('k3', {
        memory: 'on_request',
        knowledge: 'always',
        tools: 'profile',
        placement: true,
        routes: 'chat',
      }),
    )
  })

  // Raw model access is the Admin's to give a key, for coding agents (#843),
  // and not for a key pinned to a profile.
  it('lets a key drive models directly', async () => {
    vi.mocked(api.setApiKeyPermissions).mockResolvedValue(null)
    const { rerender } = render(
      <QueryClientProvider client={new QueryClient()}>
        <KeyPermissions
          apiKey={{ id: 'k4', name: 'editor', prefix: 'ygg_ghi', created_at: '2026-10-01T00:00:00Z', revoked: false }}
        />
      </QueryClientProvider>,
    )
    fireEvent.click(screen.getByLabelText('Drive models directly, for coding agents'))
    await waitFor(() =>
      expect(api.setApiKeyPermissions).toHaveBeenCalledWith('k4', {
        memory: 'on_request',
        knowledge: 'always',
        tools: 'profile',
        placement: true,
        raw: true,
      }),
    )
    rerender(
      <QueryClientProvider client={new QueryClient()}>
        <KeyPermissions
          apiKey={{
            id: 'k5',
            name: 'site',
            prefix: 'ygg_jkl',
            created_at: '2026-10-01T00:00:00Z',
            revoked: false,
            permissions: { memory: 'never', knowledge: 'never', tools: 'none', placement: false, profile: 'tires', raw: true },
          }}
        />
      </QueryClientProvider>,
    )
    const box = screen.getByLabelText('Drive models directly, for coding agents')
    expect(box).toBeDisabled()
    expect(box).not.toBeChecked()
  })
})
