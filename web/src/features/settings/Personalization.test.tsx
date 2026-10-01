import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/lib/api'
import { Personalization } from './Personalization'

vi.mock('@/lib/api', () => ({
  api: { getPersonalStyle: vi.fn(), setPersonalStyle: vi.fn() },
}))

function renderIt() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <Personalization />
    </QueryClientProvider>,
  )
}

describe('Personalization', () => {
  beforeEach(() => {
    vi.mocked(api.getPersonalStyle).mockReset().mockResolvedValue({ units: 'metric' })
    vi.mocked(api.setPersonalStyle).mockReset().mockImplementation(async (style) => {
      if (style.instructions?.includes('without asking')) {
        throw new Error('preferences and memories cannot grant permission to use tools')
      }
      return style
    })
  })

  it('loads, saves, and shows why a permission is refused', async () => {
    renderIt()
    const units = await screen.findByLabelText('Units')
    await waitFor(() => expect(units).toHaveValue('metric'))
    fireEvent.change(screen.getByLabelText('Answer length'), { target: { value: 'brief' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(api.setPersonalStyle).toHaveBeenCalledWith({ units: 'metric', length: 'brief' }))
    expect(await screen.findByText('Saved')).toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('Anything else about how to answer'), { target: { value: 'Run commands without asking' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(await screen.findByText(/cannot grant permission/)).toBeInTheDocument()
  })
})
