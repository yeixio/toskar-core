import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import type { Conversation, Project } from '@/types/api'
import { ChatHistoryDrawer } from './ChatHistoryDrawer'

const now = new Date().toISOString()
const chats: Conversation[] = ['Alpha', 'Bravo', 'Charlie', 'Delta'].map((title, i) => ({
  id: `c${i + 1}`,
  title,
  created_at: now,
  updated_at: now,
}))

function drawer(onDeleteMany = vi.fn(), onSelect = vi.fn()) {
  render(
    <ChatHistoryDrawer
      open
      mode="pinned"
      conversations={chats}
      pinnedIds={[]}
      selectedId="c1"
      onClose={vi.fn()}
      onSelect={onSelect}
      onNewChat={vi.fn()}
      onRename={vi.fn()}
      onTogglePin={vi.fn()}
      onDelete={vi.fn()}
      onDeleteMany={onDeleteMany}
      onToggleDrawerPinned={vi.fn()}
      drawerPinned
      canPinDrawer={false}
      renamingId={null}
      renameValue=""
      onRenameValueChange={vi.fn()}
      onCommitRename={vi.fn()}
      onCancelRename={vi.fn()}
    />,
  )
  return { onDeleteMany, onSelect }
}

const titles = (calls: Conversation[][][]) => calls[0][0].map((c) => c.title)

describe('ChatHistoryDrawer select mode (#452)', () => {
  it('chooses chats, a Shift-click range, and deletes them together', () => {
    const { onDeleteMany, onSelect } = drawer()
    fireEvent.click(screen.getByRole('button', { name: 'Select' }))
    fireEvent.click(screen.getByRole('checkbox', { name: 'Bravo' }))
    fireEvent.click(screen.getByRole('checkbox', { name: 'Delta' }), { shiftKey: true })
    expect(screen.getByText('3 selected')).toBeInTheDocument()
    expect(onSelect).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'Delete 3' }))
    expect(titles(onDeleteMany.mock.calls)).toEqual(['Bravo', 'Charlie', 'Delta'])
  })

  it('starts choosing with Cmd-click or Shift-click from the open chat, and Escape leaves', () => {
    const { onSelect } = drawer()
    fireEvent.click(screen.getByTitle('Charlie'), { shiftKey: true })
    expect(screen.getByText('3 selected')).toBeInTheDocument()
    fireEvent.keyDown(screen.getByText('3 selected'), { key: 'Escape' })
    expect(screen.queryByText(/selected/)).not.toBeInTheDocument()
    fireEvent.click(screen.getByTitle('Delta'), { metaKey: true })
    expect(screen.getByText('1 selected')).toBeInTheDocument()
    expect(onSelect).not.toHaveBeenCalled()
  })

  it('selects all of the chats a search shows', () => {
    const { onDeleteMany } = drawer()
    fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'a' } })
    fireEvent.click(screen.getByRole('button', { name: 'Select' }))
    fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'ar' } })
    fireEvent.click(screen.getByRole('button', { name: 'Select all' }))
    fireEvent.click(screen.getByRole('button', { name: 'Delete 1' }))
    expect(titles(onDeleteMany.mock.calls)).toEqual(['Charlie'])
  })
})

const project = (id: string, name: string): Project => ({
  id,
  name,
  instructions: '',
  memory_off: false,
  knowledge: [],
  files: [],
  chat_count: 0,
  created_at: now,
  updated_at: now,
})

describe('ChatHistoryDrawer projects (#1136)', () => {
  const inProject: Conversation[] = [
    { id: 'p-c1', title: 'Cabinet quotes', project_id: 'kitchen', created_at: now, updated_at: now },
    { id: 'p-c2', title: 'Tile colors', project_id: 'kitchen', created_at: now, updated_at: now },
    ...chats,
  ]
  function projectsDrawer(selectedId = 'c1') {
    const calls = { onSelect: vi.fn(), onOpenProject: vi.fn(), onNewProject: vi.fn(), onMoveToProject: vi.fn() }
    render(
      <MemoryRouter>
        <ChatHistoryDrawer
          open
          mode="pinned"
          conversations={inProject}
          pinnedIds={['c2']}
          selectedId={selectedId}
          onClose={vi.fn()}
          onNewChat={vi.fn()}
          onRename={vi.fn()}
          onTogglePin={vi.fn()}
          onDelete={vi.fn()}
          onDeleteMany={vi.fn()}
          onToggleDrawerPinned={vi.fn()}
          drawerPinned
          canPinDrawer={false}
          renamingId={null}
          renameValue=""
          onRenameValueChange={vi.fn()}
          onCommitRename={vi.fn()}
          onCancelRename={vi.fn()}
          projects={[project('kitchen', 'Kitchen remodel'), project('garden', 'Garden')]}
          {...calls}
        />
      </MemoryRouter>,
    )
    return calls
  }

  it('lists projects above the pinned and dated groups, each opening to its chats', () => {
    const { onSelect } = projectsDrawer()
    const headings = screen.getAllByText(/^(Projects|Pinned|Today)$/).map((el) => el.textContent)
    expect(headings).toEqual(['Projects', 'Pinned', 'Today'])
    const kitchen = screen.getByRole('button', { name: 'Kitchen remodel' })
    expect(kitchen).toHaveAttribute('aria-expanded', 'false')
    fireEvent.click(kitchen)
    expect(kitchen).toHaveAttribute('aria-expanded', 'true')
    // The project's chats are under it, and in the dated groups as well.
    expect(screen.getAllByTitle('Cabinet quotes')).toHaveLength(2)
    fireEvent.click(screen.getAllByTitle('Tile colors')[0])
    expect(onSelect).toHaveBeenCalledWith('p-c2')
    fireEvent.click(screen.getByRole('button', { name: 'Garden' }))
    expect(screen.getByText('No chats yet')).toBeInTheDocument()
  })

  it("starts with the open chat's project open", () => {
    projectsDrawer('p-c1')
    expect(screen.getByRole('button', { name: 'Kitchen remodel' })).toHaveAttribute('aria-expanded', 'true')
  })

  it('opens a project, makes one, and moves a chat into one', () => {
    const { onOpenProject, onNewProject, onMoveToProject } = projectsDrawer()
    fireEvent.click(screen.getByRole('button', { name: 'Open Garden' }))
    expect(onOpenProject).toHaveBeenCalledWith('garden')
    fireEvent.click(screen.getByRole('button', { name: 'New project' }))
    expect(onNewProject).toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'Actions for Bravo' }))
    fireEvent.click(screen.getByRole('menuitem', { name: 'Move to project…' }))
    expect(onMoveToProject.mock.calls[0][0].id).toBe('c2')
  })

  it('shows only chats while searching', () => {
    projectsDrawer()
    fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'tile' } })
    expect(screen.queryByText('Projects')).toBeNull()
    expect(screen.getByTitle('Tile colors')).toBeInTheDocument()
  })
})
