import { describe, expect, it } from 'vitest'
import { thinkingSound } from './thinking'

describe('thinkingSound', () => {
  // The same short, soft chime Core plays at the door (#1334).
  it('is short, soft and ends without a click', () => {
    const s = thinkingSound(48000)
    expect(s.length / 48000).toBeGreaterThan(0.4)
    expect(s.length / 48000).toBeLessThan(0.8)
    let peak = 0
    for (const v of s) peak = Math.max(peak, Math.abs(v))
    expect(peak).toBeCloseTo(0.12, 3)
    expect(s[0]).toBe(0)
    expect(Math.abs(s[s.length - 1])).toBeLessThan(0.001)
    expect(thinkingSound(0).length).toBe(0)
  })
})
