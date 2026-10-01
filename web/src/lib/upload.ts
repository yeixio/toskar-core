/** File types knowledge and training material accept. */
export const UPLOAD_ACCEPT = '.txt,.md,.markdown,.csv,.tsv,.json,.jsonl,.html,.htm,.xlsx,.pdf'

const BINARY = ['.xlsx', '.pdf']

export const MAX_UPLOAD_BYTES = 20 * 1024 * 1024

/** File types chat can read: documents, spreadsheets, PDFs, and code. */
export const ATTACH_ACCEPT =
  UPLOAD_ACCEPT +
  ',.py,.js,.ts,.tsx,.jsx,.go,.rs,.java,.kt,.c,.h,.cpp,.hpp,.cs,.rb,.php,.swift,.sh,.sql,.yaml,.yml,.toml,.xml,.css,.ini,.log'

export const MAX_ATTACH_BYTES = 25 * 1024 * 1024

/** Whether chat can read a file of this name. */
export function isAttachable(filename: string): boolean {
  const dot = filename.lastIndexOf('.')
  if (dot < 0) return false
  const ext = filename.slice(dot).toLowerCase()
  return ATTACH_ACCEPT.split(',').includes(ext)
}

export interface Upload {
  filename: string
  /** Set for text files. */
  text?: string
  /** Set for binary files such as spreadsheets. */
  contentBase64?: string
}

export function isBinaryUpload(filename: string): boolean {
  const lower = filename.toLowerCase()
  return BINARY.some((ext) => lower.endsWith(ext))
}

export async function readUpload(file: File): Promise<Upload> {
  if (!isBinaryUpload(file.name)) {
    return { filename: file.name, text: await file.text() }
  }
  const bytes = new Uint8Array(await file.arrayBuffer())
  let binary = ''
  const chunk = 0x8000
  for (let i = 0; i < bytes.length; i += chunk) {
    binary += String.fromCharCode(...bytes.subarray(i, i + chunk))
  }
  return { filename: file.name, contentBase64: btoa(binary) }
}
