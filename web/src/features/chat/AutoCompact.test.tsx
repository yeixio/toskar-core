import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { ContextUsageButton } from './ContextUsageButton'
import { SummaryDialog } from './SummaryDialog'

vi.mock('@/lib/api', () => ({
  api: { editConversationSummary: vi.fn((id: string, summary: string) => Promise.resolve({ conversation_id: id, summary, edited: true })) },
}))

import { api } from '@/lib/api'

const wrap = (ui: React.ReactNode) => <QueryClientProvider client={new QueryClient()}>{ui}</QueryClientProvider>

describe('auto-compact (#342)', () => {
  it('explains it in the gauge and offers its actions', () => {
    const compact = { hasSummary: true, off: false, busy: false, onView: vi.fn(), onCompactNow: vi.fn(), onNewChat: vi.fn(), onVerbatim: vi.fn() }
    render(<ContextUsageButton usage={null} windowLimit={8192} compact={compact} />)
    fireEvent.click(screen.getByRole('button', { name: 'Context usage' }))
    expect(screen.getByText(/Long chats are compacted/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'View summary' }))
    fireEvent.click(screen.getByRole('button', { name: 'Compact now' }))
    fireEvent.click(screen.getByRole('button', { name: 'New chat from summary' }))
    fireEvent.click(screen.getByRole('checkbox', { name: /Keep this chat verbatim/ }))
    expect(compact.onView).toHaveBeenCalled()
    expect(compact.onCompactNow).toHaveBeenCalled()
    expect(compact.onNewChat).toHaveBeenCalled()
    expect(compact.onVerbatim).toHaveBeenCalledWith(true)
  })

  it('hides Compact now for a chat kept verbatim, and summary actions before one', () => {
    const compact = { hasSummary: false, off: true, busy: false, onView: vi.fn(), onCompactNow: vi.fn(), onNewChat: vi.fn(), onVerbatim: vi.fn() }
    render(<ContextUsageButton usage={null} windowLimit={8192} compact={compact} />)
    fireEvent.click(screen.getByRole('button', { name: 'Context usage' }))
    expect(screen.queryByRole('button', { name: 'Compact now' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'View summary' })).toBeNull()
    expect(screen.getByRole('checkbox', { name: /Keep this chat verbatim/ })).toBeChecked()
  })

  it('shows the summary and saves a correction', async () => {
    const summary = {
      conversation_id: 'c1',
      through_message_id: 'm4',
      through_created_at: '',
      message_count: 4,
      summary: 'The dog is Max.',
      updated_at: '',
      edited: false,
    }
    render(wrap(<SummaryDialog summary={summary} onClose={vi.fn()} />))
    expect(screen.getByText('The dog is Max.')).toBeInTheDocument()
    expect(screen.getByText(/in place of the first 4 messages/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Edit' }))
    fireEvent.change(screen.getByRole('textbox'), { target: { value: 'The dog is Rex.' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(api.editConversationSummary).toHaveBeenCalledWith('c1', 'The dog is Rex.'))
  })
})
