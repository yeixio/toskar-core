import { describe, expect, it } from 'vitest'
import { isTeamProfile, roleDisplayName, strategyLabel } from './profilePresentation'

describe('profile strategy', () => {
  it('treats the Team strategy and the old Team orchestrator alike', () => {
    expect(isTeamProfile({ orchestrator_id: 'simple', orchestration: { strategy: 'team' } })).toBe(true)
    expect(isTeamProfile({ orchestrator_id: 'team' })).toBe(true)
    expect(isTeamProfile({ orchestrator_id: 'simple' })).toBe(false)
    expect(isTeamProfile(undefined)).toBe(false)
  })

  it('labels each strategy', () => {
    expect(strategyLabel({ orchestrator_id: 'simple' }, false).title).toBe('Automatic')
    expect(strategyLabel({ orchestrator_id: 'simple', orchestration: { strategy: 'single' } }, true).title).toBe(
      'Single model · Strategy: Single model',
    )
    expect(strategyLabel({ orchestrator_id: 'team' }, false).title).toBe('AI team')
  })

  it('names the model roles', () => {
    expect(roleDisplayName('assistant')).toBe('Primary')
    expect(roleDisplayName('planner')).toBe('Planner')
    expect(roleDisplayName('worker:2')).toBe('Worker 2')
    expect(roleDisplayName('custom-step')).toBe('Custom-step')
  })
})
