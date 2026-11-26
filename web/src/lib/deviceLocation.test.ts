import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { deviceLocationOn, locate, positionForMessage, resetDeviceLocation, setDeviceLocationOn } from './deviceLocation'

describe('Use this device\'s location (#1310)', () => {
  let calls = 0
  beforeEach(() => {
    calls = 0
    resetDeviceLocation()
    setDeviceLocationOn(false)
    Object.defineProperty(navigator, 'geolocation', {
      configurable: true,
      value: {
        getCurrentPosition: (ok: PositionCallback) => {
          calls++
          ok({ coords: { latitude: 58.3019, longitude: -134.4197, accuracy: 35.4 } } as GeolocationPosition)
        },
      },
    })
  })
  afterEach(() => vi.useRealTimers())

  it('sends nothing while it is off', () => {
    expect(deviceLocationOn()).toBe(false)
    expect(positionForMessage()).toBeUndefined()
    expect(calls).toBe(0)
  })

  it('sends the last position once it is on, and asks again when it is old', async () => {
    vi.useFakeTimers()
    setDeviceLocationOn(true)
    await locate()
    expect(positionForMessage()).toEqual({ latitude: 58.3019, longitude: -134.4197, accuracy_m: 35 })
    expect(calls).toBe(1)
    vi.advanceTimersByTime(11 * 60_000)
    positionForMessage()
    expect(calls).toBe(2)
    setDeviceLocationOn(false)
    expect(positionForMessage()).toBeUndefined()
  })
})
