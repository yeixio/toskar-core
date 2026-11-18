import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { applyLanguage } from '@/i18n'
import { ToolPermissionPrompt, type PendingToolPrompt } from './ToolPermissionPrompt'
import { shotsFrom } from './videoShots'

const base: PendingToolPrompt = { requestId: 'r1', toolId: 'terminal', argsSummary: 'Run a command' }

function renderIt(tool: PendingToolPrompt) {
  const onDecide = vi.fn()
  render(<ToolPermissionPrompt tool={tool} toolName="Terminal" showRawArgs={false} deciding={false} onDecide={onDecide} />)
  return onDecide
}

describe('ToolPermissionPrompt', () => {
  beforeEach(async () => {
    await applyLanguage('en')
  })

  it('says allowing the terminal for the session trusts the project', () => {
    renderIt(base)
    expect(screen.getByText(/run the project's own code/)).toBeInTheDocument()
  })

  it('offers to allow a tool for the session', () => {
    const onDecide = renderIt(base)
    fireEvent.click(screen.getByRole('button', { name: 'Allow for session' }))
    expect(onDecide).toHaveBeenCalledWith(true, true)
    expect(screen.getByText('Run a command')).toBeInTheDocument()
  })

  it("shows the exact command that can't be undone, and offers no allow for session (#1017)", () => {
    const onDecide = renderIt({ ...base, command: 'rm -rf build', alwaysAsks: true, cantUndo: true })
    expect(screen.getByText('rm -rf build')).toBeInTheDocument()
    expect(screen.queryByText('Run a command')).not.toBeInTheDocument()
    expect(screen.getByText(/This can't be undone/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Allow for session' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Allow once' }))
    expect(onDecide).toHaveBeenCalledWith(true, false)
  })

  it('says when it asks because the chat read something from outside', () => {
    renderIt({ ...base, command: 'git status', alwaysAsks: true, afterOutside: true })
    expect(screen.getByText(/just read something from outside/)).toBeInTheDocument()
    expect(screen.queryByText(/can't tell this command is harmless/)).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Allow for session' })).not.toBeInTheDocument()
  })

  it("says why a command it can't vouch for asks each time", () => {
    renderIt({ ...base, command: 'unknown-tool', alwaysAsks: true })
    expect(screen.getByText(/can't tell this command is harmless/)).toBeInTheDocument()
    expect(screen.queryByText(/can't be undone/)).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Allow for session' })).not.toBeInTheDocument()
  })

  it("shows a longer video's shots and how long it takes before anything is made (#790)", () => {
    const shots = shotsFrom({ shots: ['Sunrise over the bay', { prompt: 'Boats head out', seconds: 3 }, { prompt: ' ' }, { prompt: 'Too long', seconds: 9 }] })
    expect(shots).toEqual([
      { prompt: 'Sunrise over the bay', seconds: 5 },
      { prompt: 'Boats head out', seconds: 3 },
      { prompt: 'Too long', seconds: 5 },
    ])
    renderIt({ ...base, toolId: 'video.shots', argsSummary: '', shots, style: 'golden light', videoSeconds: 13, estimateMinutes: 195 })
    expect(screen.getByText('3 shots, about 13 seconds of video')).toBeInTheDocument()
    expect(screen.getByText(/About 3 h 15 min to make on this computer/)).toBeInTheDocument()
    expect(screen.getByText('In every shot: golden light')).toBeInTheDocument()
    const list = screen.getByRole('list', { name: 'Shots' })
    expect(list.querySelectorAll('li')).toHaveLength(3)
    expect(list).toHaveTextContent('Boats head out · 3 s')
  })

  it("says a longer video's time isn't known before this computer has made a clip", () => {
    renderIt({ ...base, toolId: 'video.shots', argsSummary: '', shots: [{ prompt: 'Rain', seconds: 5 }] })
    expect(screen.getByText('1 shot, about 5 seconds of video')).toBeInTheDocument()
    expect(screen.getByText(/hasn't made a clip yet/)).toBeInTheDocument()
    expect(shotsFrom({ shots: 'not a list' })).toBeUndefined()
    expect(shotsFrom({ shots: [{ prompt: '' }] })).toBeUndefined()
  })
})
