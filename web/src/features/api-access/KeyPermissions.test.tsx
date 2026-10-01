import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { api } from '@/lib/api'
import { KeyPermissions } from './KeyPermissions'

vi.mock('@/lib/api', () => ({ api: { setApiKeyPermissions: vi.fn() } }))

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
})
