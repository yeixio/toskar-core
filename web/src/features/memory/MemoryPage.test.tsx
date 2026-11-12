import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import { api } from '@/lib/api'
import type { MemoryItem, Project, SettingsView } from '@/types/api'
import { MemoryToggle } from '@/features/chat/MemoryToggle'
import { AnswerDetails } from '@/features/chat/AnswerDetails'
import { MemoryPage } from './MemoryPage'

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    api: { ...actual.api, listMemory: vi.fn(), listProjects: vi.fn(), addMemory: vi.fn(), updateMemory: vi.fn(), deleteMemory: vi.fn(), getSettings: vi.fn(), updateSettings: vi.fn() },
  }
})

const mem = (id: string, content: string, category: MemoryItem['category']): MemoryItem => ({
  id, content, category, source_type: 'explicit', enabled: true, created_at: '2026-09-30T00:00:00Z', updated_at: '2026-09-30T00:00:00Z',
})

function wrap(ui: React.ReactNode) {
  return render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <MemoryRouter>{ui}</MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('MemoryPage', () => {
  it('groups memories, adds one, and deletes one', async () => {
    vi.mocked(api.listMemory).mockResolvedValue({
      memories: [mem('1', 'I prefer metric units', 'preferences'), mem('2', 'This project uses Go', 'projects')],
      categories: ['identity', 'preferences', 'projects', 'technical', 'interests', 'people', 'other'],
    })
    vi.mocked(api.getSettings).mockResolvedValue({ memory_enabled: true } as SettingsView)
    vi.mocked(api.addMemory).mockResolvedValue(mem('3', 'I am vegetarian', 'identity'))
    vi.mocked(api.deleteMemory).mockResolvedValue(null)
    wrap(<MemoryPage />)
    await waitFor(() => expect(screen.getByText('I prefer metric units')).toBeInTheDocument())
    expect(screen.getByRole('heading', { name: 'Preferences' })).toBeInTheDocument()
    expect(screen.getAllByText(/You asked in a chat/)).toHaveLength(2)

    fireEvent.change(screen.getByPlaceholderText('I prefer short answers with examples'), { target: { value: 'I am vegetarian' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add' }))
    await waitFor(() => expect(api.addMemory).toHaveBeenCalledWith('I am vegetarian', undefined))

    // Delete is in each memory's menu.
    fireEvent.click(screen.getAllByRole('button', { name: 'More actions for this memory' })[1])
    fireEvent.click(screen.getByRole('menuitem', { name: 'Delete memory: This project uses Go' }))
    await waitFor(() => expect(api.deleteMemory).toHaveBeenCalledWith('2'))
  })
})

describe('MemoryPage, projects', () => {
  it("lists a project's memories under it and moves one to personal memory", async () => {
    vi.mocked(api.listMemory).mockResolvedValue({
      memories: [
        mem('1', 'I prefer metric units', 'preferences'),
        { ...mem('2', 'The garden soil is clay', 'projects'), project_id: 'proj-house' },
        { ...mem('3', 'Old idea', 'other'), project_id: 'proj-gone' },
      ],
      categories: ['identity', 'preferences', 'projects', 'technical', 'interests', 'people', 'other'],
    })
    vi.mocked(api.getSettings).mockResolvedValue({ memory_enabled: true } as SettingsView)
    vi.mocked(api.listProjects).mockResolvedValue([{ id: 'proj-house', name: 'House', instructions: '', memory_off: false, knowledge: [] } as unknown as Project])
    vi.mocked(api.updateMemory).mockResolvedValue({ ...mem('2', 'The garden soil is clay', 'projects') })
    wrap(<MemoryPage />)
    const house = await screen.findByRole('link', { name: 'Project: House' })
    expect(house).toHaveAttribute('href', '/projects/proj-house')
    // A project's memory isn't among the person's own.
    expect(screen.queryByRole('heading', { name: 'Projects' })).toBeNull()
    const section = screen.getByRole('region', { name: 'Project: House' })
    expect(section).toHaveTextContent('The garden soil is clay')
    // A project that's gone still shows its memories, to move or delete.
    expect(screen.getByRole('heading', { name: 'Project: A project' })).toBeInTheDocument()

    // Only a project's memory can move.
    fireEvent.click(screen.getAllByRole('button', { name: 'More actions for this memory' })[0])
    expect(screen.queryByRole('menuitem', { name: 'Move to personal memory' })).toBeNull()
    fireEvent.click(screen.getAllByRole('button', { name: 'More actions for this memory' })[1])
    fireEvent.click(screen.getByRole('menuitem', { name: 'Move to personal memory' }))
    await waitFor(() => expect(api.updateMemory).toHaveBeenCalledWith('2', { project_id: '' }))
  })
})

describe('MemoryPage, explained', () => {
  it('explains memory and offers examples when nothing is remembered', async () => {
    vi.mocked(api.listMemory).mockResolvedValue({ memories: [], categories: ['preferences', 'other'] })
    vi.mocked(api.getSettings).mockResolvedValue({ memory_enabled: true } as SettingsView)
    wrap(<MemoryPage />)
    expect(await screen.findByRole('heading', { name: 'How memory works' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Knowledge' })).toHaveAttribute('href', '/knowledge')
    fireEvent.click(screen.getByRole('button', { name: 'I prefer metric units.' }))
    expect(screen.getByPlaceholderText('I prefer short answers with examples')).toHaveValue('I prefer metric units.')
  })

  it('turns memory off for every chat with the switch', async () => {
    vi.mocked(api.listMemory).mockResolvedValue({ memories: [], categories: ['other'] })
    vi.mocked(api.getSettings).mockResolvedValue({ memory_enabled: true } as SettingsView)
    vi.mocked(api.updateSettings).mockResolvedValue({ memory_enabled: false } as SettingsView)
    wrap(<MemoryPage />)
    const toggle = await screen.findByRole('switch', { name: 'Use memory in chats' })
    await waitFor(() => expect(toggle).toBeEnabled())
    fireEvent.click(toggle)
    await waitFor(() => expect(api.updateSettings).toHaveBeenCalledWith({ memory_enabled: false }))
  })
})

describe('MemoryToggle', () => {
  it('turns memory off for a chat and links to settings when off everywhere', () => {
    const onChange = vi.fn()
    const { rerender } = wrap(<MemoryToggle memoryEnabled off={false} onChange={onChange} />)
    fireEvent.click(screen.getByRole('button', { name: /Memory on/ }))
    expect(onChange).toHaveBeenCalledWith(true)
    rerender(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter>
          <MemoryToggle memoryEnabled={false} off={false} onChange={onChange} />
        </MemoryRouter>
      </QueryClientProvider>,
    )
    expect(screen.getByRole('link', { name: 'Memory off' })).toHaveAttribute('href', '/memory')
  })
})

describe('memory sources', () => {
  it('shows memories used for an answer as one chip', () => {
    render(
      <AnswerDetails
        meta={{
          sources: [
            { kind: 'memory', title: 'I prefer metric units', source: 'Memory' },
            { kind: 'memory', title: 'My name is Sam', source: 'Memory' },
          ],
          steps: [{ kind: 'memory', text: 'Used 2 memories' }],
        }}
      />,
    )
    const chip = screen.getByRole('button', { name: /Memory/ })
    expect(chip).toHaveTextContent('· 2')
    fireEvent.click(chip)
    expect(screen.getByText('I prefer metric units')).toBeInTheDocument()
  })

  it('says which language a memory in another language is written in', async () => {
    vi.mocked(api.listMemory).mockResolvedValue({
      memories: [
        { ...mem('1', 'Mi proyecto usa Go.', 'projects'), language: 'es' },
        { ...mem('2', 'I prefer metric units', 'preferences'), language: 'en' },
      ],
      categories: ['identity', 'preferences', 'projects', 'technical', 'interests', 'people', 'other'],
    })
    vi.mocked(api.getSettings).mockResolvedValue({ memory_enabled: true } as SettingsView)
    wrap(<MemoryPage />)
    expect(await screen.findByText('Mi proyecto usa Go.')).toBeInTheDocument()
    const label = screen.getByText((_, el) => el?.tagName === 'SPAN' && el.getAttribute('lang') === 'es')
    expect(label).toHaveTextContent('Español')
    // A memory in the App language says nothing more.
    expect(document.querySelector('span[lang="en"]')).toBeNull()
  })
})
