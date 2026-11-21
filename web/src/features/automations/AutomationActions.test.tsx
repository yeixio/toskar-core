import { fireEvent, render, screen } from '@testing-library/react'
import { useState } from 'react'
import { describe, expect, it } from 'vitest'
import type { AutomationAction, Device } from '@/types/api'
import { AutomationActions } from './AutomationActions'

const devices: Device[] = [
  { connector: 'homeassistant', device: 'light.porch', name: 'Porch light', kind: 'light', state: 'off' },
  { connector: 'homeassistant', device: 'lock.front_door', name: 'Front door', kind: 'lock', state: 'locked' },
  { connector: 'homeassistant', device: 'camera.door', name: 'Door camera', kind: 'camera', state: 'idle' },
  { connector: 'homeassistant', device: 'binary_sensor.motion', name: 'Motion', kind: 'sensor', state: 'off' },
]

function Harness({ initial = [], list = devices, seen }: { initial?: AutomationAction[]; list?: Device[]; seen: AutomationAction[][] }) {
  const [value, setValue] = useState(initial)
  return (
    <AutomationActions
      devices={list}
      value={value}
      onChange={(next) => {
        seen.push(next)
        setValue(next)
      }}
    />
  )
}

describe('AutomationActions', () => {
  it('allows a device action picked from the connected devices', () => {
    const seen: AutomationAction[][] = []
    render(<Harness seen={seen} />)
    const device = screen.getByLabelText('Device')
    // Cameras and sensors can't be acted on.
    expect(screen.queryByRole('option', { name: 'Door camera' })).not.toBeInTheDocument()
    expect(screen.queryByRole('option', { name: 'Motion' })).not.toBeInTheDocument()
    fireEvent.change(device, { target: { value: 'light.porch' } })
    fireEvent.click(screen.getByRole('button', { name: 'Allow' }))
    expect(seen.at(-1)).toEqual([{ tool: 'homeassistant.call', args: { service: 'light.turn_on', entity_id: 'light.porch' } }])
    expect(screen.getByText('Turn on: Porch light')).toBeInTheDocument()

    fireEvent.change(device, { target: { value: 'lock.front_door' } })
    fireEvent.change(screen.getByLabelText('Action'), { target: { value: 'lock' } })
    fireEvent.click(screen.getByRole('button', { name: 'Allow' }))
    expect(seen.at(-1)?.[1]).toEqual({ tool: 'homeassistant.call', args: { service: 'lock.lock', entity_id: 'lock.front_door' } })

    fireEvent.click(screen.getByRole('button', { name: 'Remove Turn on: Porch light' }))
    expect(seen.at(-1)).toEqual([{ tool: 'homeassistant.call', args: { service: 'lock.lock', entity_id: 'lock.front_door' } }])
  })

  it('shows saved actions even when their devices are gone, and nothing without devices', () => {
    const { container } = render(<Harness seen={[]} list={[]} />)
    expect(container).toBeEmptyDOMElement()
    render(<Harness seen={[]} list={[]} initial={[{ tool: 'github.comment', args: { repo: 'o/r' } }]} />)
    expect(screen.getByText('github.comment (repo o/r)')).toBeInTheDocument()
  })
})
