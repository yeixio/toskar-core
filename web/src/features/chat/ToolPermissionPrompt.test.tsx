import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { applyLanguage } from '@/i18n'
import { ToolPermissionPrompt, type PendingToolPrompt } from './ToolPermissionPrompt'

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
})
