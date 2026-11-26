import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { applyLanguage } from '@/i18n'
import { api } from '@/lib/api'
import { RatingsSettings } from './RatingsSettings'

vi.mock('@/lib/api', () => ({ api: { getSettings: vi.fn(), updateSettings: vi.fn() } }))

describe('RatingsSettings (#1391)', () => {
  beforeEach(async () => {
    await applyLanguage('en')
    vi.mocked(api.getSettings).mockResolvedValue({ community_ratings: false, quality_results: true } as never)
    vi.mocked(api.updateSettings).mockReset().mockResolvedValue({} as never)
  })

  it('turns community ratings on from the Models page', async () => {
    const onClose = vi.fn()
    render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <RatingsSettings onClose={onClose} />
      </QueryClientProvider>,
    )
    const show = await screen.findByRole('switch', { name: 'Show community ratings' })
    await waitFor(() => expect(show).toBeEnabled())
    expect(show).toHaveAttribute('aria-checked', 'false')
    expect(screen.getByRole('switch', { name: 'Ask me to rate models' })).toHaveAttribute('aria-checked', 'true')
    fireEvent.click(show)
    await waitFor(() => expect(api.updateSettings).toHaveBeenCalledWith({ community_ratings: true }))
    fireEvent.click(screen.getByRole('button', { name: 'Done' }))
    expect(onClose).toHaveBeenCalled()
  })
})
