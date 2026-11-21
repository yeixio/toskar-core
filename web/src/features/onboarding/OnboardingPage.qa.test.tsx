import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import { OnboardingPage } from './OnboardingPage'


vi.mock('@/lib/api', () => ({ api: new Proxy({} as Record<string, unknown>, { get: (target, name: string) => (target[name] ??= vi.fn(async () => undefined)) }) }))
vi.mock('@/lib/events', () => ({ subscribeEvents: () => () => {} }))

describe('Onboarding QA regressions', () => {
  // Regression: toskar-core#1252
  it('the first step starts at the top, so a tall step scrolls instead of cutting off the mascot', () => {
    render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter>
          <OnboardingPage />
        </MemoryRouter>
      </QueryClientProvider>,
    )
    // justify-center on a scrolling column pushes the top of tall content
    // out of reach; safe centering (or margin auto inside) doesn't.
    const main = screen.getByRole('main')
    expect(main.className.split(/\s+/)).not.toContain('justify-center')
  })

  // Regression: toskar-core#1266
  it('someone who opened another page can set up later and land there', () => {
    render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter initialEntries={['/onboarding?next=%2Fapi-access']}>
          <Routes>
            <Route path="/onboarding" element={<OnboardingPage />} />
            <Route path="/api-access" element={<p>API Access page</p>} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )
    fireEvent.click(screen.getByRole('button', { name: 'Set up later' }))
    expect(screen.getByText('API Access page')).toBeInTheDocument()
  })
})
