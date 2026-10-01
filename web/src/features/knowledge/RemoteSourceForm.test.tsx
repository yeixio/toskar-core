import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { api } from '@/lib/api'
import type { KnowledgeSource } from '@/types/api'
import { RemoteSourceForm } from './RemoteSourceForm'

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return { ...actual, api: { ...actual.api, createKnowledge: vi.fn() } }
})

const renderForm = (kind: 'database' | 'api', onAdded = vi.fn()) =>
  render(
    <QueryClientProvider client={new QueryClient()}>
      <RemoteSourceForm kind={kind} onAdded={onAdded} />
    </QueryClientProvider>,
  )

describe('RemoteSourceForm', () => {
  it('connects a PostgreSQL query with its connection string', async () => {
    vi.mocked(api.createKnowledge).mockResolvedValue({ id: 'k', status: 'ready' } as KnowledgeSource)
    const onAdded = vi.fn()
    renderForm('database', onAdded)
    fireEvent.change(screen.getByRole('combobox', { name: 'Database' }), { target: { value: 'postgres' } })
    fireEvent.change(screen.getByPlaceholderText(/postgres:\/\//), { target: { value: 'postgres://reader:pw@db/shop' } })
    fireEvent.change(screen.getByRole('textbox', { name: 'Query' }), { target: { value: 'SELECT sku, price FROM products' } })
    fireEvent.click(screen.getByRole('button', { name: 'Connect' }))
    await waitFor(() =>
      expect(api.createKnowledge).toHaveBeenCalledWith({
        kind: 'database',
        name: undefined,
        remote: { driver: 'postgres', connection_string: 'postgres://reader:pw@db/shop', query: 'SELECT sku, price FROM products', refresh_minutes: 60 },
      }),
    )
    expect(onAdded).toHaveBeenCalled()
  })

  it('connects an API with a token header and shows a failed first fetch', async () => {
    vi.mocked(api.createKnowledge).mockResolvedValue({ id: 'k', status: 'failed', error: 'shop.example answered 401' } as KnowledgeSource)
    renderForm('api')
    expect(screen.getByRole('button', { name: 'Connect' })).toBeDisabled()
    fireEvent.change(screen.getByPlaceholderText(/https:\/\/shop/), { target: { value: 'https://shop.example/api/stock' } })
    fireEvent.change(screen.getByPlaceholderText('Bearer …'), { target: { value: 'Bearer abc' } })
    fireEvent.click(screen.getByRole('button', { name: 'Connect' }))
    await waitFor(() =>
      expect(api.createKnowledge).toHaveBeenLastCalledWith({
        kind: 'api',
        name: undefined,
        remote: { url: 'https://shop.example/api/stock', items: undefined, headers: { Authorization: 'Bearer abc' }, refresh_minutes: 60 },
      }),
    )
    expect(await screen.findByText('shop.example answered 401')).toBeInTheDocument()
  })
})
