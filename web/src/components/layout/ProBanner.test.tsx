import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/lib/api'
import type { ProLicense } from '@/types/api'
import { ProBanner } from './ProBanner'

vi.mock('@/lib/api', () => ({ api: { getLicense: vi.fn() } }))

function show(license: ProLicense) {
  vi.mocked(api.getLicense).mockResolvedValue(license)
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <ProBanner />
    </QueryClientProvider>,
  )
}

type Shell = { go?: { main?: { App?: Record<string, () => Promise<string>> } } }

afterEach(() => {
  delete (window as Shell).go
})

// Toskar Pro's trial (yeixio/toskar-apps#336).
describe('ProBanner', () => {
  it('shows nothing for Core on its own or an unlocked Pro', async () => {
    show({ kind: '', limited: false })
    await waitFor(() => expect(api.getLicense).toHaveBeenCalled())
    expect(screen.queryByRole('status')).toBeNull()
  })

  it('counts the trial down, and says where to unlock in a browser', async () => {
    show({ kind: 'trial', limited: false, days_left: 9, store: 'mac' })
    expect(await screen.findByText('Toskar Pro trial: 9 days left.')).toBeTruthy()
    expect(screen.getByText('Unlock it in Toskar Pro on this computer.')).toBeTruthy()
  })

  it('unlocks through the desktop app once the trial has ended', async () => {
    const unlock = vi.fn(async () => 'unlocked')
    ;(window as Shell).go = {
      main: { App: { ProUnlockPrice: async () => '$24.99', ProUnlock: unlock, ProRestore: async () => 'none' } },
    }
    show({ kind: 'ended', limited: true, store: 'mac' })
    expect(await screen.findByText(/Your Toskar Pro trial has ended/)).toBeTruthy()
    fireEvent.click(await screen.findByRole('button', { name: 'Unlock for $24.99' }))
    expect(await screen.findByText('Toskar Pro is unlocked.')).toBeTruthy()
    expect(unlock).toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'Restore purchase' }))
    expect(await screen.findByText('No Unlock was found for the store account on this computer.')).toBeTruthy()
  })
})
