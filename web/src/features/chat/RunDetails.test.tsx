import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { api } from '@/lib/api'
import { useUIStore } from '@/stores/uiStore'
import { AnswerDetails } from './AnswerDetails'

vi.mock('@/lib/api', () => ({ api: { getRun: vi.fn() } }))

describe('Run details', () => {
  it('appear in advanced mode and load the trace when opened', async () => {
    vi.mocked(api.getRun).mockResolvedValue({
      id: 'abcdef123456',
      strategy: ['Looked up the web first'],
      effort: 'Balanced',
      status: 'completed',
      started_at: '2026-10-01T20:00:00Z',
      latency_ms: 4200,
      models: [{ model_id: 'llama-3.2-1b-q4', role: 'assistant', node: 'This Mac', calls: 2, load_ms: 1800, first_token_ms: 2100, prompt_tokens: 900, completion_tokens: 80, cached_tokens: 600, tok_per_sec: 61.2 }],
      tools: [{ tool_id: 'internet.search', calls: 1, total_ms: 900 }],
      nodes: ['This Mac'],
      verification_passes: 1,
      retries: 0,
    })
    const meta = { run_id: 'abcdef123456' }
    const { rerender } = render(<AnswerDetails meta={meta} />)
    expect(screen.queryByText(/Run details/)).not.toBeInTheDocument()

    useUIStore.setState({ advancedMode: true })
    rerender(<AnswerDetails meta={meta} />)
    fireEvent.click(screen.getByRole('button', { name: /Run details/ }))
    expect(await screen.findByText(/llama-3.2-1b-q4 on This Mac · 2 calls · load 1.8 s/)).toBeInTheDocument()
    expect(screen.getByText(/600 cached/)).toBeInTheDocument()
    expect(screen.getByText('internet.search ×1 (900 ms)')).toBeInTheDocument()
    expect(screen.getByText('Looked up the web first')).toBeInTheDocument()
    expect(api.getRun).toHaveBeenCalledWith('abcdef123456')
    useUIStore.setState({ advancedMode: false })
  })
})
