import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it } from 'vitest'
import type { HardwareInventory, Node } from '@/types/api'
import { ModelsTargetBar } from './ModelsTargetBar'

describe('ModelsTargetBar QA regressions', () => {
  // Regression: toskar-core#1248
  it("calls this computer by its Toskar name, not the OS hostname", () => {
    const nodes = [{ id: 'n1', name: 'qa-box', is_local: true, paired: true, status: 'online' } as Node]
    render(
      <MemoryRouter>
        <ModelsTargetBar target="local" onTargetChange={() => {}} localHardware={{ hostname: 'vm' } as HardwareInventory} nodes={nodes} running={[]} />
      </MemoryRouter>,
    )
    expect(screen.getByRole('option', { name: 'qa-box' })).toBeInTheDocument()
    expect(screen.queryByText(/\bvm\b/)).not.toBeInTheDocument()
  })
})
