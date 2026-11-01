import { vi } from 'vitest'

/**
 * Ratatoskr holds still with reduced motion. Otherwise jsdom runs his
 * animation frames off a faked setInterval, about 60 frames a second of fake
 * time, and advancing a minute takes seconds on a busy runner. Undo with
 * vi.unstubAllGlobals().
 */
export function stillMascot() {
  vi.stubGlobal('matchMedia', (query: string) => ({ matches: query.includes('reduce'), media: query, addEventListener() {}, removeEventListener() {} }))
}
