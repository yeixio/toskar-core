import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { api } from '@/lib/api'
import type { Node } from '@/types/api'
import { NodesPage } from './NodesPage'

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      getNodes: vi.fn(),
      listPairingOffers: vi.fn(),
      listRunningModels: vi.fn(),
      getModels: vi.fn(),
      revokeNode: vi.fn(),
      getSettings: vi.fn(),
      getDevicePairing: vi.fn(),
      listJoinTokens: vi.fn(),
      pairNode: vi.fn(),
      deleteModel: vi.fn(),
    },
  }
})

const local = { id: 'local', name: 'This Mac', is_local: true, paired: true, status: 'online' } as Node
const studio = { id: 'studio', name: 'Studio', is_local: false, paired: true, status: 'online' } as Node

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <NodesPage />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('NodesPage', () => {
  beforeEach(() => {
    vi.mocked(api.getNodes).mockResolvedValue([local, studio])
    vi.mocked(api.listPairingOffers).mockResolvedValue([])
    vi.mocked(api.listRunningModels).mockResolvedValue([])
    vi.mocked(api.getModels).mockResolvedValue([])
    vi.mocked(api.getSettings).mockResolvedValue({} as never)
    vi.mocked(api.revokeNode).mockResolvedValue(null as never)
    vi.mocked(api.getDevicePairing).mockResolvedValue(null as never)
    vi.mocked(api.listJoinTokens).mockResolvedValue([] as never)
  })
  afterEach(() => vi.restoreAllMocks())

  it('is titled like the sidebar', async () => {
    renderPage()
    expect(screen.getByRole('heading', { level: 1, name: 'Computers' })).toBeInTheDocument()
    expect(await screen.findByRole('heading', { name: 'Studio' })).toBeInTheDocument()
  })

  it('explains how to add a computer while this is the only one, with the real button names', async () => {
    vi.mocked(api.getNodes).mockResolvedValue([local])
    renderPage()
    expect(await screen.findByRole('heading', { name: 'Add another computer' })).toBeInTheDocument()
    expect(screen.getByText(/choose Add to team, then approve it on the other computer/)).toBeInTheDocument()
    expect(screen.getByText(/use Add by command/)).toBeInTheDocument()
  })

  it('asks before removing a computer from the team', async () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValueOnce(false).mockReturnValueOnce(true)
    renderPage()
    const more = await screen.findByRole('button', { name: 'More actions for Studio' })
    fireEvent.click(more)
    fireEvent.click(screen.getByRole('menuitem', { name: 'Remove from team' }))
    expect(confirm).toHaveBeenCalledTimes(1)
    expect(api.revokeNode).not.toHaveBeenCalled()
    fireEvent.click(more)
    fireEvent.click(screen.getByRole('menuitem', { name: 'Remove from team' }))
    await waitFor(() => expect(api.revokeNode).toHaveBeenCalledWith('studio'))
  })

  it('highlights the header button for the open panel, one panel at a time', async () => {
    renderPage()
    await screen.findByRole('heading', { name: 'Studio' })
    const join = screen.getByRole('button', { name: 'Add by command' })
    const phone = screen.getByRole('button', { name: 'Connect a device' })
    const find = screen.getByRole('button', { name: 'Find computers' })

    // No panel open: Find computers is the one primary action.
    expect(join).toHaveAttribute('aria-pressed', 'false')
    expect(phone).toHaveAttribute('aria-pressed', 'false')
    expect(find).toHaveClass('btn-primary')

    fireEvent.click(phone)
    expect(phone).toHaveAttribute('aria-pressed', 'true')
    expect(phone).toHaveAttribute('aria-controls', 'computers-panel')
    expect(join).toHaveAttribute('aria-pressed', 'false')
    expect(find).toHaveClass('btn-secondary')
    expect(find).not.toHaveClass('btn-primary')
    expect(screen.getByRole('heading', { name: 'Connect a device' })).toBeInTheDocument()

    // Picking the other panel moves the highlight and closes the first.
    fireEvent.click(join)
    expect(join).toHaveAttribute('aria-pressed', 'true')
    expect(phone).toHaveAttribute('aria-pressed', 'false')
    expect(screen.queryByRole('heading', { name: 'Connect a device' })).not.toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Add a computer by command' })).toBeInTheDocument()

    // Clicking the open one again closes it and Find computers is primary again.
    fireEvent.click(join)
    expect(join).toHaveAttribute('aria-pressed', 'false')
    expect(join).not.toHaveAttribute('aria-controls')
    expect(find).toHaveClass('btn-primary')
  })
})

describe('NodesPage QA regressions', () => {
  const nearbyA = { id: 'a', name: 'Attic PC', is_local: false, paired: false, status: 'online', address: '192.168.1.30:7331' } as Node
  const nearbyB = { id: 'b', name: 'Basement PC', is_local: false, paired: false, status: 'online', address: '192.168.1.31:7331' } as Node
  beforeEach(() => {
    vi.mocked(api.getNodes).mockResolvedValue([local, nearbyA, nearbyB])
    vi.mocked(api.listPairingOffers).mockResolvedValue([])
    vi.mocked(api.listRunningModels).mockResolvedValue([])
    vi.mocked(api.getModels).mockResolvedValue([])
    vi.mocked(api.getSettings).mockResolvedValue({} as never)
    vi.mocked(api.getDevicePairing).mockResolvedValue(null as never)
    vi.mocked(api.listJoinTokens).mockResolvedValue([] as never)
  })
  afterEach(() => vi.restoreAllMocks())

  // Regression: toskar-core#1245
  it('adding one nearby computer shows busy on that card only', async () => {
    vi.mocked(api.pairNode).mockReturnValue(new Promise(() => {}))
    renderPage()
    const buttons = await screen.findAllByRole('button', { name: 'Add to team' })
    expect(buttons).toHaveLength(2)
    fireEvent.click(buttons[0])
    await screen.findAllByRole('button', { name: 'Connecting…' })
    expect(screen.getAllByRole('button', { name: 'Connecting…' })).toHaveLength(1)
    expect(screen.getByRole('button', { name: 'Add to team' })).toBeEnabled()
  })

  // Regression: toskar-core#1246
  it('one way to add a nearby computer, with the code behind a link', async () => {
    vi.mocked(api.getNodes).mockResolvedValue([local, nearbyA])
    renderPage()
    await screen.findByRole('button', { name: 'Add to team' })
    expect(screen.queryByRole('button', { name: 'Approve with code' })).not.toBeInTheDocument()
  })

  // Regression: toskar-core#1247
  it('Remove asks before deleting a model from a computer', async () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    vi.mocked(api.getNodes).mockResolvedValue([local])
    vi.mocked(api.getModels).mockResolvedValue([
      { id: 'llama-3.2-1b', name: 'Llama 3.2 1B', installed_on: [{ node_id: 'local' }] },
    ] as never)
    vi.mocked(api.deleteModel).mockResolvedValue(undefined as never)
    renderPage()
    fireEvent.click(await screen.findByRole('button', { name: 'Manage' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Remove' }))
    await new Promise((resolve) => setTimeout(resolve, 50))
    expect(api.deleteModel).not.toHaveBeenCalled()
    confirm.mockRestore()
  })
})
