import { useState } from 'react'
import { downloadArtifact } from '@/lib/api'
import type { FileRef } from '@/types/api'

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

const kindLabel: Record<string, string> = {
  spreadsheet: 'Spreadsheet',
  pdf: 'PDF',
  document: 'Document',
  code: 'Code',
  image: 'Image',
}

function FileIcon({ kind }: { kind: string }) {
  const tone =
    kind === 'spreadsheet' ? 'text-success' : kind === 'pdf' ? 'text-danger' : kind === 'code' ? 'text-bifrost' : 'text-mimir'
  return (
    <svg viewBox="0 0 16 16" className={`h-4 w-4 shrink-0 ${tone}`} fill="none" stroke="currentColor" strokeWidth={1.4} aria-hidden>
      <path d="M4 1.5h5.5L13 5v9.5H4z" strokeLinejoin="round" />
      <path d="M9.5 1.5V5H13" strokeLinejoin="round" />
      {kind === 'spreadsheet' ? <path d="M6 8h5M6 10.5h5M8.5 7v5" /> : <path d="M6 8.5h4.5M6 11h3" />}
    </svg>
  )
}

/** A stored file. Clicking it downloads the file. */
export function FileChip({ file }: { file: FileRef }) {
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  return (
    <button
      type="button"
      className="inline-flex max-w-[18rem] items-center gap-2 rounded-lg border border-line/70 bg-surface px-2.5 py-1.5 text-left text-xs text-ink transition hover:border-primary/50"
      title={error ?? `Download ${file.name}`}
      disabled={busy}
      onClick={async () => {
        setBusy(true)
        setError(null)
        try {
          await downloadArtifact(file)
        } catch (err) {
          setError(err instanceof Error ? err.message : 'The download failed.')
        } finally {
          setBusy(false)
        }
      }}
    >
      <FileIcon kind={file.kind} />
      <span className="min-w-0">
        <span className="block truncate font-medium">{file.name}</span>
        <span className={`block text-[11px] ${error ? 'text-danger' : 'text-ink-faint'}`}>
          {error ?? `${kindLabel[file.kind] ?? 'File'} · ${formatSize(file.size_bytes)}${busy ? ' · Downloading…' : ''}`}
        </span>
      </span>
    </button>
  )
}

/** A file waiting in the composer: uploading, ready, or failed. */
export interface PendingFile {
  key: string
  name: string
  size: number
  status: 'uploading' | 'ready' | 'error'
  /** Set once the upload finishes. */
  file?: FileRef
  error?: string
}

export function PendingFileChip({ file, onRemove }: { file: PendingFile; onRemove: () => void }) {
  return (
    <span
      className={[
        'inline-flex max-w-[18rem] items-center gap-2 rounded-lg border bg-surface px-2.5 py-1.5 text-xs',
        file.status === 'error' ? 'border-danger/60' : 'border-line/70',
      ].join(' ')}
      title={file.error ?? file.name}
    >
      <FileIcon kind={file.name.toLowerCase().endsWith('.pdf') ? 'pdf' : 'document'} />
      <span className="min-w-0">
        <span className="block truncate font-medium text-ink">{file.name}</span>
        <span className={`block truncate text-[11px] ${file.status === 'error' ? 'text-danger' : 'text-ink-faint'}`}>
          {file.status === 'uploading' ? 'Adding…' : file.status === 'error' ? file.error : formatSize(file.size)}
        </span>
      </span>
      <button
        type="button"
        className="ml-1 shrink-0 rounded px-1 text-ink-faint hover:text-ink"
        aria-label={`Remove ${file.name}`}
        onClick={onRemove}
      >
        ×
      </button>
    </span>
  )
}
