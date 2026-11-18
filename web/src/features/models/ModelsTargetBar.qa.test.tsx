import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect } from 'vitest'
import { knownBug } from '@/test/knownBug'
import type { HardwareInventory, Node } from '@/types/api'
import { ModelsTargetBar } from './ModelsTargetBar'

// QA regressions for the Models page. knownBug passes while the bug is
// there and fails once it's fixed (src/test/knownBug.ts).
describe('ModelsTargetBar QA regressions', () => {
  // Regression: toskar-core#1248
  knownBug('toskar-core#1248', "calls this computer by its Toskar name, not the OS hostname", () => {
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
