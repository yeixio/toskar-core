import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { ContextUsageButton } from './ContextUsageButton'
import type { ContextUsage } from './contextUsage'

describe('Context usage QA regressions', () => {
  // Regression: toskar-core#1254
  it('says how full it is beside the meter, so a low one is not a spinner', () => {
    const usage = { promptTokens: 410, limit: 8192, instructions: 200, tools: 100, conversation: 110, toolResults: 0, estimated: false } as ContextUsage
    render(<ContextUsageButton usage={usage} windowLimit={8192} />)
    expect(screen.getByRole('button').textContent).toMatch(/5\s?%/)
  })
})
