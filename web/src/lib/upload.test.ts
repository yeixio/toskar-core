import { describe, expect, it } from 'vitest'
import { isAttachable, isBinaryUpload, readUpload } from './upload'

// jsdom's File lacks text() and arrayBuffer(), which browsers provide.
function fakeFile(name: string, bytes: number[]): File {
  const data = new Uint8Array(bytes)
  return {
    name,
    size: data.length,
    text: async () => new TextDecoder().decode(data),
    arrayBuffer: async () => data.buffer,
  } as unknown as File
}

describe('readUpload', () => {
  it('reads text files as text and spreadsheets as base64', async () => {
    const csv = [...new TextEncoder().encode('a,b\n1,2\n')]
    expect(await readUpload(fakeFile('prices.csv', csv))).toEqual({ filename: 'prices.csv', text: 'a,b\n1,2\n' })
    expect(await readUpload(fakeFile('Stock.XLSX', [80, 75, 3, 4, 255]))).toEqual({
      filename: 'Stock.XLSX',
      contentBase64: 'UEsDBP8=',
    })
    expect(isBinaryUpload('notes.md')).toBe(false)
  })

  it('knows which files chat can read', () => {
    expect(isAttachable('Report.PDF')).toBe(true)
    expect(isAttachable('main.go')).toBe(true)
    expect(isAttachable('photo.png')).toBe(false)
    expect(isAttachable('README')).toBe(false)
  })
})
