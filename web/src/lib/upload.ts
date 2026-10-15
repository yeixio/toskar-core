/** File types knowledge and training material accept. */
export const UPLOAD_ACCEPT = '.txt,.md,.markdown,.csv,.tsv,.json,.jsonl,.html,.htm,.xlsx,.pdf,.docx,.pptx'

/** Audio chat can play and transcribe. */
export const AUDIO_ACCEPT = '.wav,.mp3,.m4a,.aac,.ogg,.flac,.webm'

/** Images chat can show and edit. */
export const IMAGE_ACCEPT = '.png,.jpg,.jpeg'

/** Videos chat can show to a model that sees, as frames. (.webm is with audio.) */
export const VIDEO_ACCEPT = '.mp4,.mov,.m4v,.mkv'

const BINARY = ['.xlsx', '.pdf', '.docx', '.pptx', ...AUDIO_ACCEPT.split(','), ...IMAGE_ACCEPT.split(','), ...VIDEO_ACCEPT.split(',')]

export const MAX_UPLOAD_BYTES = 20 * 1024 * 1024

/** File types chat can read: documents, spreadsheets, PDFs, code, audio, images, and videos. */
export const ATTACH_ACCEPT =
  UPLOAD_ACCEPT +
  ',.py,.js,.ts,.tsx,.jsx,.go,.rs,.java,.kt,.c,.h,.cpp,.hpp,.cs,.rb,.php,.swift,.sh,.sql,.yaml,.yml,.toml,.xml,.css,.ini,.log,' +
  AUDIO_ACCEPT +
  ',' +
  IMAGE_ACCEPT +
  ',' +
  VIDEO_ACCEPT

/** Whether a file is a PNG or JPEG image, by its name. */
export function isImageName(filename: string): boolean {
  const dot = filename.lastIndexOf('.')
  return dot >= 0 && IMAGE_ACCEPT.split(',').includes(filename.slice(dot).toLowerCase())
}

/** Whether a file is audio, by its name. */
export function isAudioName(filename: string): boolean {
  const dot = filename.lastIndexOf('.')
  return dot >= 0 && AUDIO_ACCEPT.split(',').includes(filename.slice(dot).toLowerCase())
}

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

/**
 * The file types someone may attach (#589): files, pictures and videos to
 * look at, and recordings, as their capabilities allow; '' for none.
 */
export function attachAccept(allow: { files: boolean; pictures: boolean; voice: boolean }): string {
  if (allow.files && allow.pictures && allow.voice) return ATTACH_ACCEPT
  const parts: string[] = []
  if (allow.files) {
    parts.push(UPLOAD_ACCEPT, '.py,.js,.ts,.tsx,.jsx,.go,.rs,.java,.kt,.c,.h,.cpp,.hpp,.cs,.rb,.php,.swift,.sh,.sql,.yaml,.yml,.toml,.xml,.css,.ini,.log')
  }
  if (allow.voice) parts.push(AUDIO_ACCEPT)
  if (allow.pictures) parts.push(IMAGE_ACCEPT, VIDEO_ACCEPT)
  return parts.join(',')
}
