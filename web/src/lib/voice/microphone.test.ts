import { afterEach, describe, expect, it } from 'vitest'
import { readMicrophoneChoice, saveMicrophoneChoice } from './audio'

describe('microphone choice', () => {
  afterEach(() => localStorage.clear())

  // The system's microphone with auto gain until one is chosen on this device (#1330).
  it('defaults, and remembers a choice', () => {
    expect(readMicrophoneChoice()).toEqual({ deviceId: '', autoGain: true })
    saveMicrophoneChoice({ deviceId: 'webcam', autoGain: false })
    expect(readMicrophoneChoice()).toEqual({ deviceId: 'webcam', autoGain: false })
  })

  it('ignores a damaged choice', () => {
    localStorage.setItem('toskar.voice.microphone', '{not json')
    expect(readMicrophoneChoice()).toEqual({ deviceId: '', autoGain: true })
  })
})
