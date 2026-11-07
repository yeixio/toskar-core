import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { api } from '@/lib/api'
import type { AIProfile, SettingsView } from '@/types/api'
import { ProfilesPage } from './ProfilesPage'
import { defaultProfileIdOf, freeProfileName } from './profilePresentation'

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      getProfiles: vi.fn(),
      getModels: vi.fn(),
      getNodes: vi.fn(),
      listKnowledge: vi.fn(),
      getSettings: vi.fn(),
      updateSettings: vi.fn(),
    },
  }
})

const profile = (id: string, name: string): AIProfile => ({
  id,
  name,
  purpose: 'general',
  orchestrator_id: 'simple',
  roles: [{ role: 'assistant', model_id: '', required: false }],
  node_policy: { mode: 'automatic' },
  tools: [],
})

const general = profile('general-assistant', 'General Assistant')
const shop = profile('shop', 'Shop help')

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <ProfilesPage />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

// The card that shows a profile's name.
const card = async (name: string) => (await screen.findByRole('heading', { name })).closest('article, li, section, div.card') as HTMLElement

describe('The default profile for new chats', () => {
  beforeEach(() => {
    vi.mocked(api.getProfiles).mockResolvedValue([general, shop])
    vi.mocked(api.getModels).mockResolvedValue([])
    vi.mocked(api.getNodes).mockResolvedValue([])
    vi.mocked(api.listKnowledge).mockResolvedValue([])
    vi.mocked(api.getSettings).mockResolvedValue({} as SettingsView)
    vi.mocked(api.updateSettings).mockResolvedValue({} as SettingsView)
  })

  it('is General Assistant until another is chosen, and any other can be made the default', async () => {
    renderPage()
    expect(within(await card('General Assistant')).getByText('Default for new chats')).toBeTruthy()
    expect(within(await card('Shop help')).queryByText('Default for new chats')).toBeNull()

    fireEvent.click(screen.getByRole('button', { name: 'More actions for Shop help' }))
    fireEvent.click(screen.getByRole('menuitem', { name: 'Use for new chats' }))
    await waitFor(() => expect(api.updateSettings).toHaveBeenCalledWith({ default_profile_id: 'shop' }))
  })

  it('shows the chosen one, and the default has no menu item to make it the default', async () => {
    vi.mocked(api.getSettings).mockResolvedValue({ default_profile_id: 'shop' } as SettingsView)
    renderPage()
    await waitFor(async () => expect(within(await card('Shop help')).queryByText('Default for new chats')).toBeTruthy())
    expect(within(await card('General Assistant')).queryByText('Default for new chats')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'More actions for Shop help' }))
    expect(screen.queryByRole('menuitem', { name: 'Use for new chats' })).toBeNull()
  })
})

describe('Profile names', () => {
  it('picks a name no other profile has', () => {
    expect(freeProfileName('Shop help', [general])).toBe('Shop help')
    expect(freeProfileName('General Assistant', [general])).toBe('General Assistant (2)')
    expect(freeProfileName('general assistant', [general, profile('x', 'General Assistant (2)')])).toBe('general assistant (3)')
  })

  it('falls back to General Assistant when the chosen profile is gone', () => {
    expect(defaultProfileIdOf('shop', [general, shop])).toBe('shop')
    expect(defaultProfileIdOf('gone', [general])).toBe('general-assistant')
    expect(defaultProfileIdOf(undefined, [general])).toBe('general-assistant')
  })
})
