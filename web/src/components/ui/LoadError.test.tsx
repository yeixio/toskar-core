import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import { LoadError } from './LoadError'

function show(node: React.ReactNode) {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <MemoryRouter>{node}</MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('LoadError compact (#217)', () => {
  it('says what failed in one line, with Try again and Report this', () => {
    const onRetry = vi.fn()
    show(<LoadError compact error={new Error('Failed to fetch')} onRetry={onRetry} />)
    expect(screen.getByRole('alert')).toHaveTextContent("Toskar couldn't load this")
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }))
    expect(onRetry).toHaveBeenCalledOnce()
    expect(screen.getByRole('button', { name: 'Report this' })).toBeInTheDocument()
    expect(screen.queryByRole('heading')).not.toBeInTheDocument()
  })

  it('is busy while retrying', () => {
    show(<LoadError compact error="x" onRetry={() => {}} retrying />)
    expect(screen.getByRole('button', { name: 'Trying again…' })).toBeDisabled()
  })
})
