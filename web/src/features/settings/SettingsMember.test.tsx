import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { applyLanguage } from '@/i18n'
import { api } from '@/lib/api'
import { SettingsPage } from './SettingsPage'

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  const refused = () => Promise.reject(new actual.ApiError(403, 'this needs the admin role', 'ROLE_REQUIRED'))
  return {
    ...actual,
    api: new Proxy({ getMe: vi.fn(), getSettings: vi.fn(), setMyPreferences: vi.fn() } as Record<string, unknown>, {
      get: (target, key: string) => target[key] ?? refused,
    }),
  }
})

function renderAs(section: 'notifications' | 'voice') {
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <MemoryRouter initialEntries={[`/settings/${section}`]}>
        <SettingsPage section={section} />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('Notifications and Voice for a Member (#1391)', () => {
  beforeEach(async () => {
    await applyLanguage('en')
    vi.mocked(api.getMe).mockResolvedValue({
      person: { id: 'sam', name: 'Sam', username: 'sam', role: 'member', created_at: '', sign_in: true },
      via: 'session',
    })
    vi.mocked(api.getSettings).mockResolvedValue({ notify_task_finish: true, thinking_sound: true } as never)
    vi.mocked(api.setMyPreferences).mockReset().mockResolvedValue({} as never)
  })

  it('lets them turn their own automation notices off, and nothing of the Admin’s', async () => {
    renderAs('notifications')
    const own = await screen.findByRole('switch', { name: 'Notify when tasks finish' })
    await waitFor(() => expect(own).toBeEnabled())
    expect(screen.queryByRole('switch', { name: /offline/i })).not.toBeInTheDocument()
    expect(screen.getByText(/are set by an Admin/)).toBeInTheDocument()
    fireEvent.click(own)
    await waitFor(() => expect(api.setMyPreferences).toHaveBeenCalledWith({ notify_task_finish: false }))
  })

  it('lets them set their thinking sound, and says the voices are an Admin’s', async () => {
    renderAs('voice')
    const sound = await screen.findByRole('switch', { name: 'Thinking sound' })
    await waitFor(() => expect(sound).toBeEnabled())
    expect(screen.getByText(/The voices Toskar speaks in/)).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: /How voice hears you/i })).not.toBeInTheDocument()
    fireEvent.click(sound)
    await waitFor(() => expect(api.setMyPreferences).toHaveBeenCalledWith({ thinking_sound: false }))
  })
  it('lets them pick a character for voice chat (#1369)', async () => {
    renderAs('voice')
    const pick = await screen.findByRole('combobox', { name: 'Avatar' })
    await waitFor(() => expect(pick).toBeEnabled())
    expect(pick).toHaveValue('none')
    fireEvent.change(pick, { target: { value: 'lumen' } })
    await waitFor(() => expect(api.setMyPreferences).toHaveBeenCalledWith({ voice_avatar: 'lumen' }))
  })
})
