import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, vi } from 'vitest'
import { knownBug } from '@/test/knownBug'
import { OnboardingPage } from './OnboardingPage'

// QA regressions for onboarding. knownBug passes while the bug is there and
// fails once it's fixed (src/test/knownBug.ts).

vi.mock('@/lib/api', () => ({ api: new Proxy({} as Record<string, unknown>, { get: (target, name: string) => (target[name] ??= vi.fn(async () => undefined)) }) }))
vi.mock('@/lib/events', () => ({ subscribeEvents: () => () => {} }))

describe('Onboarding QA regressions', () => {
  // Regression: toskar-core#1252
  knownBug('toskar-core#1252', 'the first step starts at the top, so a tall step scrolls instead of cutting off the mascot', () => {
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
})
