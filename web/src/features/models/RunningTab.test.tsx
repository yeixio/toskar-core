import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it } from 'vitest'
import { applyLanguage } from '@/i18n'
import { formatBytes } from '@/lib/format'
import type { HardwareInventory, RunningModelView } from '@/types/api'
import { RunningTab } from './RunningTab'

const GB = 1024 ** 3

const model = (over: Partial<RunningModelView>): RunningModelView => ({
  model_id: 'qwen',
  display_name: 'Qwen 3.5 9B',
  instance_id: 'i1',
  node_id: 'n1',
  node_name: 'Rig',
  status: 'running',
  ...over,
})

// A computer with 64 GB and a 24 GB graphics card.
const rig = {
  memory: { total_bytes: 64 * GB },
  accelerators: [{ kind: 'gpu', dedicated_vram_bytes: 24 * GB }],
} as unknown as HardwareInventory

function show(item: RunningModelView) {
  render(
    <MemoryRouter>
      <RunningTab running={[item]} profiles={[]} localHardware={rig} onStop={() => {}} />
    </MemoryRouter>,
  )
}

describe('RunningTab memory', () => {
  beforeEach(async () => {
    await applyLanguage('en')
  })

  // What the model's process holds, measured (#1066), is the computer's
  // memory, so it's shown against that and not the card's.
  it('shows the measured memory against the computer’s memory', () => {
    show(model({ memory_bytes: 8 * GB, resident_bytes: 6 * GB }))
    expect(screen.getByText(`${formatBytes(6 * GB)} / ${formatBytes(64 * GB)}`)).toBeInTheDocument()
  })

  it('shows the estimate against the card until it’s measured', () => {
    show(model({ memory_bytes: 8 * GB }))
    expect(screen.getByText(`${formatBytes(8 * GB)} / ${formatBytes(24 * GB)}`)).toBeInTheDocument()
  })
})
