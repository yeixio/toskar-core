import { describe, expect, it } from 'vitest'
import { addOutput, folderName, OUTPUT_LIMIT, plainOutput } from './outputText'

describe('plainOutput', () => {
  it('takes out colors, cursor moves and titles', () => {
    expect(plainOutput('\x1b[31mFAIL\x1b[0m ok\x1b[2K')).toBe('FAIL ok')
    expect(plainOutput('\x1b]0;a title\x07done')).toBe('done')
  })

  it('shows a progress bar once', () => {
    expect(plainOutput('10%\r50%\r100%\ndone\n')).toBe('100%\ndone\n')
  })

  it('keeps Windows line ends from emptying a line', () => {
    expect(plainOutput('one\r\ntwo\r\n')).toBe('one\ntwo\n')
  })

  it('keeps tabs and takes out other controls', () => {
    expect(plainOutput('a\tb\x07\x00c')).toBe('a\tbc')
  })
})

describe('addOutput', () => {
  it('keeps the end of long output', () => {
    const kept = addOutput('start', 'x'.repeat(OUTPUT_LIMIT))
    expect(kept.length).toBe(OUTPUT_LIMIT)
    expect(kept.startsWith('start')).toBe(false)
  })
})

describe('folderName', () => {
  it('is the last part of a path', () => {
    expect(folderName('/home/sam/work/')).toBe('work')
    expect(folderName('C:\\Users\\sam\\work')).toBe('work')
    expect(folderName('/')).toBe('/')
  })
})
