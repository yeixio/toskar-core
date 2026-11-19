import { describe, expect, it } from 'vitest'
import { downloadBytes, installAction, modelToolAssessment, needsTightFitInstallWarning, sizeLabel, speedLabel } from './modelPresentation'
import type { Model, ModelFit } from '@/types/api'

function model(partial: Partial<Model>): Model {
  return {
    id: 'm',
    display_name: 'Model',
    capabilities: { tool_calling: false, vision: false, coding: false },
    installed: false,
    ...partial,
  }
}

describe('modelToolAssessment', () => {
  it('says tool-calling models can fetch the web with a terminal command', () => {
    const got = modelToolAssessment(model({ capabilities: { tool_calling: true, vision: false, coding: true } }))
    expect(got.summary).toBe('Uses local tools · Can fetch the web')
    expect(got.detail).toContain('curl')
  })

  it('says web is blocked when the profile turns the terminal off', () => {
    const got = modelToolAssessment(
      model({ capabilities: { tool_calling: true, vision: false, coding: false } }),
      { terminalAllowed: false },
    )
    expect(got.summary).toBe('Uses local tools · Web blocked by this profile')
  })

  it('marks models without tool calling as chat only', () => {
    const got = modelToolAssessment(model({}))
    expect(got.summary).toBe('Chat only · No web access')
    expect(got.detail).toContain('cannot look anything up on the web')
  })
})

function fit(partial: Partial<ModelFit>): ModelFit {
  return {
    model_id: 'qwen',
    label: 'tight',
    expected_memory_bytes: 1,
    install_allowed: true,
    ...partial,
  }
}

describe('needsTightFitInstallWarning', () => {
  it('warns only before a tight fit model is installed', () => {
    expect(needsTightFitInstallWarning(model({ installed: false }), fit({ label: 'tight' }))).toBe(true)
    expect(needsTightFitInstallWarning(model({ installed: false }), fit({ label: 'excellent' }))).toBe(false)
    expect(needsTightFitInstallWarning(model({ installed: false }), fit({ label: 'good' }))).toBe(false)
    expect(needsTightFitInstallWarning(model({ installed: true }), fit({ label: 'tight' }))).toBe(false)
  })
})

describe('installAction', () => {
  it('keeps a tight or heavy model installable', () => {
    expect(installAction(fit({ label: 'tight' })).disabled).toBe(false)
    expect(installAction(fit({ label: 'heavy' }))).toEqual({
      label: 'Install anyway',
      disabled: false,
    })
  })

  it('blocks only an unsupported model', () => {
    expect(installAction(fit({ label: 'unsupported', install_allowed: false }))).toEqual({
      label: 'Unsupported',
      disabled: true,
    })
  })

  it('does not treat a legacy too-large label as a hard block', () => {
    expect(installAction(fit({ label: 'too_large', install_allowed: undefined })).disabled).toBe(false)
  })
})

describe('speedLabel', () => {
  it('labels a guess and a measurement differently', () => {
    expect(speedLabel(fit({ est_tok_per_sec: 26 }))).toBe('Estimated ~26 tok/s')
    expect(speedLabel(fit({ est_tok_per_sec: 26.4, tok_per_sec_measured: true }))).toBe(
      'Measured 26.4 tok/s',
    )
  })
})

// Regression: toskar-core#1343
describe('model sizes', () => {
  it('shows one labeled download figure, with the memory it needs', () => {
    const qwen = { id: 'q', display_name: 'Qwen', installed: false, capabilities: {}, size_bytes: 3 * 1024 ** 3, download_bytes: 3.5 * 1024 ** 3, memory_needed_bytes: 4.7 * 1024 ** 3 } as Model
    expect(downloadBytes(qwen)).toBe(3.5 * 1024 ** 3)
    expect(sizeLabel(qwen)).toMatch(/^3\.5 GB download · about 4\.7 GB of memory$/)
    expect(downloadBytes({ size_bytes: 10 })).toBe(10)
  })
})
