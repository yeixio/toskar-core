import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { api } from '@/lib/api'
import { saveText } from '@/lib/desktopBridge'
import type { SampleFile } from '@/types/api'

async function download(file: SampleFile) {
  // The desktop app's web view can't download; its shell saves the file.
  if ((await saveText(file.filename, file.content)) !== null) return
  const url = URL.createObjectURL(new Blob([file.content], { type: 'text/plain' }))
  const a = document.createElement('a')
  a.href = url
  a.download = file.filename
  a.click()
  URL.revokeObjectURL(url)
}

/** Example material in each format, to read or download as a starting point. */
export function SampleFiles({ open = false }: { open?: boolean }) {
  const samples = useQuery({ queryKey: ['training', 'samples'], queryFn: () => api.trainingSamples(), staleTime: Infinity })
  const [shown, setShown] = useState<string | null>(null)
  const [saveError, setSaveError] = useState<string | null>(null)
  return (
    <details className="text-sm" open={open}>
      <summary className="cursor-pointer text-ink">See sample files</summary>
      <p className="mt-2 text-ink-muted">What good material looks like. Download one as a template for your own data.</p>
      {saveError && <p className="mt-2 text-xs text-danger">{saveError}</p>}
      <ul className="mt-2 space-y-2">
        {(samples.data ?? []).map((f) => {
          const lines = f.content.split('\n')
          return (
            <li key={f.filename} className="rounded-lg bg-raised p-3">
              <div className="flex flex-wrap items-center gap-2">
                <span className="font-medium text-ink">{f.name}</span>
                <span className="mono-id">{f.filename}</span>
                <span className="ml-auto flex gap-1.5">
                  <button type="button" className="btn-secondary px-2 py-1 text-xs" onClick={() => setShown(shown === f.filename ? null : f.filename)}>
                    {shown === f.filename ? 'Hide' : 'Preview'}
                  </button>
                  <button
                    type="button"
                    className="btn-secondary px-2 py-1 text-xs"
                    onClick={() => {
                      setSaveError(null)
                      download(f).catch((err) => setSaveError(err instanceof Error ? err.message : String(err)))
                    }}
                  >
                    Download
                  </button>
                </span>
              </div>
              <p className="mt-1 text-xs text-ink-muted">{f.description}</p>
              {shown === f.filename && (
                <pre className="log-panel mt-2 max-h-64 overflow-auto whitespace-pre text-xs">
                  {lines.slice(0, 12).join('\n')}
                  {lines.length > 13 ? `\n… ${lines.length - 13} more lines` : ''}
                </pre>
              )}
            </li>
          )
        })}
      </ul>
    </details>
  )
}
