import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { applyLanguage } from '@/i18n'
import { api } from '@/lib/api'
import { useUIStore } from '@/stores/uiStore'
import type { SettingsView } from '@/types/api'
import { LanguageSettings } from './LanguageSettings'

vi.mock('@/lib/api', () => ({ api: { getSettings: vi.fn(), updateSettings: vi.fn() } }))

function renderIt() {
  return render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <LanguageSettings />
    </QueryClientProvider>,
  )
}

beforeEach(() => {
  vi.mocked(api.getSettings).mockResolvedValue({ ui_locale: '' } as SettingsView)
  vi.mocked(api.updateSettings).mockImplementation(async (patch) => ({ ui_locale: patch.ui_locale }) as SettingsView)
})

afterEach(async () => {
  useUIStore.getState().setAdvancedMode(false)
  await applyLanguage('en')
  localStorage.clear()
})

describe('LanguageSettings', () => {
  it('offers the system default, named in its language, and the languages with a catalog', async () => {
    renderIt()
    const select = await screen.findByRole('combobox', { name: 'App language' })
    await waitFor(() => expect(select).toBeEnabled())
    const options = [...select.querySelectorAll('option')].map((o) => o.textContent)
    expect(options).toEqual(['System default (English)', 'English'])
  })

  it('saves the choice on the daemon and shows it at once', async () => {
    useUIStore.getState().setAdvancedMode(true) // the pseudo-locale is offered in advanced mode
    renderIt()
    const select = await screen.findByRole('combobox', { name: 'App language' })
    await waitFor(() => expect(select).toBeEnabled())
    fireEvent.change(select, { target: { value: 'en-XA' } })
    await waitFor(() => expect(api.updateSettings).toHaveBeenCalledWith({ ui_locale: 'en-XA' }))
    // The section itself is now pseudo-localized.
    expect(await screen.findByText(/^\[!! .*Ļáá/)).toBeInTheDocument()
    expect(document.documentElement.lang).toBe('en-XA')
  })
})
