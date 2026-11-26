// Use this device's location (#1310): when the person turns it on, on this
// device, the chat sends where the device is with each message, so a
// question about nearby places, the weather, or directions starts from
// where they are now. The browser asks before it says, once or each time,
// as the person chose there. The computer uses it for that message only
// and keeps none of it.

export interface DevicePosition {
  latitude: number
  longitude: number
  accuracy_m?: number
}

const settingKey = 'toskar.deviceLocation'
/** A position this old is asked for again. */
const freshMs = 10 * 60_000

let last: { position: DevicePosition; at: number } | null = null
let asking: Promise<DevicePosition> | null = null

function read(): string | null {
  try {
    return window.localStorage.getItem(settingKey)
  } catch {
    return null
  }
}

/** Whether the person turned on Use this device's location on this device. */
export function deviceLocationOn(): boolean {
  return read() === 'on'
}

export function setDeviceLocationOn(on: boolean) {
  try {
    if (on) window.localStorage.setItem(settingKey, 'on')
    else window.localStorage.removeItem(settingKey)
  } catch {
    // Private windows can refuse storage: it stays off.
  }
  if (!on) last = null
}

/** Whether this browser can say where it is at all. */
export function canLocate(): boolean {
  return typeof navigator !== 'undefined' && !!navigator.geolocation
}

/**
 * Asks the device where it is. The browser asks the person first, the
 * first time or each time. A town's precision is plenty, so it doesn't
 * ask for GPS.
 */
export function locate(): Promise<DevicePosition> {
  if (!canLocate()) return Promise.reject(new Error('unavailable'))
  asking ??= new Promise<DevicePosition>((resolve, reject) => {
    navigator.geolocation.getCurrentPosition(
      (p) => {
        const position = { latitude: p.coords.latitude, longitude: p.coords.longitude, accuracy_m: Math.round(p.coords.accuracy) }
        last = { position, at: Date.now() }
        resolve(position)
      },
      (err) => reject(err),
      { enableHighAccuracy: false, maximumAge: freshMs, timeout: 10_000 },
    )
  }).finally(() => {
    asking = null
  })
  return asking
}

/**
 * Where the device was last, for a message being sent now, or undefined
 * when the setting is off or nothing is known yet. Sending never waits:
 * an old or missing position is asked for again for the next message.
 */
export function positionForMessage(): DevicePosition | undefined {
  if (!deviceLocationOn()) return undefined
  if (!last || Date.now() - last.at > freshMs) void locate().catch(() => {})
  return last?.position
}

/** Forgets the last position: tests. */
export function resetDeviceLocation() {
  last = null
  asking = null
}
