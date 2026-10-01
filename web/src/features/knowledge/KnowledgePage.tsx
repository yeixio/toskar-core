import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { EmptyState } from '@/components/ui/EmptyState'
import { api } from '@/lib/api'
import { isBinaryUpload, readUpload, UPLOAD_ACCEPT } from '@/lib/upload'
import type { KnowledgeSource } from '@/types/api'
import { errorText } from '@/features/train/display'
import { RealmKicker } from '@/components/ui/Realm'


export function KnowledgePage() {
  const queryClient = useQueryClient()
  const sources = useQuery({ queryKey: ['knowledge'], queryFn: () => api.listKnowledge() })
  const refresh = () => void queryClient.invalidateQueries({ queryKey: ['knowledge'] })
  const list = sources.data ?? []

  return (
    <div className="page-fill gap-4 overflow-y-auto p-4">
      <div>
        <RealmKicker />
        <h1 className="font-display text-2xl font-semibold text-ink">Knowledge</h1>
        <p className="mt-1 max-w-2xl text-sm text-ink-muted">
          Mimir keeps catalogs, prices, policies, and documents searchable. When a profile or a specialized AI uses a
          source, the passages that match each question are added to it. Edit a file and the next question sees the
          change. No retraining.
        </p>
      </div>
      <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_minmax(20rem,26rem)]">
        <section className="space-y-3">
          {sources.isLoading && <p className="text-sm text-ink-muted">Loading…</p>}
          {!sources.isLoading && list.length === 0 && (
            <EmptyState
              title="No knowledge connected"
              description="Connect a file or folder on this computer, or paste content. PDF, Excel, CSV, TSV, JSON, JSONL, Markdown, text, and HTML files work."
            />
          )}
          <ul className="space-y-2">
            {list.map((s) => (
              <SourceRow key={s.id} source={s} onChanged={refresh} />
            ))}
          </ul>
          {list.length > 0 && <SearchBox />}
        </section>
        <aside>
          <AddSource onAdded={refresh} />
        </aside>
      </div>
    </div>
  )
}

function SourceRow({ source, onChanged }: { source: KnowledgeSource; onChanged: () => void }) {
  const [draft, setDraft] = useState<string | null>(null)
  const open = useMutation({
    mutationFn: () => api.knowledgeContent(source.id),
    onSuccess: (res) => setDraft(res?.text ?? ''),
  })
  const save = useMutation({
    mutationFn: (text: string) => api.updateKnowledge(source.id, { text }),
    onSuccess: () => {
      setDraft(null)
      onChanged()
    },
  })
  const reindex = useMutation({ mutationFn: () => api.refreshKnowledge(source.id), onSuccess: onChanged })
  const remove = useMutation({ mutationFn: () => api.deleteKnowledge(source.id), onSuccess: onChanged })
  return (
    <li className="card !p-4">
      <div className="flex flex-wrap items-start gap-3">
        <div className="min-w-[12rem] flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <span className="font-medium text-ink">{source.name}</span>
            <span className="badge-mimir">{source.kind === 'path' ? 'Linked' : 'Copy'}</span>
            {source.status === 'failed' && <span className="status-chip bg-danger/15 text-danger">Failed</span>}
          </div>
          <p className="mt-0.5 break-anywhere text-xs text-ink-muted">
            {source.kind === 'path' ? source.path : source.filename} · {source.chunk_count} passages
            {source.refreshed_at && ` · indexed ${new Date(source.refreshed_at).toLocaleString()}`}
          </p>
          {source.kind === 'path' && (
            <p className="mt-0.5 text-xs text-ink-faint">Reindexes itself when the files change.</p>
          )}
          {source.error && <p className="mt-1 text-xs text-danger">{source.error}</p>}
        </div>
        <div className="flex shrink-0 gap-1.5">
          {source.kind === 'text' && draft == null && !isBinaryUpload(source.filename ?? '') && (
            <button type="button" className="btn-secondary px-2 py-1 text-xs" disabled={open.isPending} onClick={() => open.mutate()}>
              Edit
            </button>
          )}
          <button type="button" className="btn-secondary px-2 py-1 text-xs" disabled={reindex.isPending} onClick={() => reindex.mutate()}>
            {reindex.isPending ? 'Indexing…' : 'Reindex'}
          </button>
          <button
            type="button"
            className="btn-secondary px-2 py-1 text-xs"
            disabled={remove.isPending}
            onClick={() => {
              if (window.confirm(`Disconnect ${source.name}? Profiles and AIs that use it stop seeing it. Your files are not deleted.`)) remove.mutate()
            }}
          >
            Disconnect
          </button>
        </div>
      </div>
      {draft != null && (
        <div className="mt-3 space-y-2">
          <textarea className="field min-h-48 w-full font-mono text-xs" value={draft} onChange={(e) => setDraft(e.target.value)} aria-label={`Edit ${source.name}`} />
          <p className="text-xs text-ink-faint">Saving reindexes the source. Chats and specialized AIs see the change on their next question.</p>
          <div className="flex gap-1.5">
            <button type="button" className="btn-primary px-3 py-1 text-xs" disabled={save.isPending} onClick={() => save.mutate(draft)}>
              {save.isPending ? 'Saving…' : 'Save'}
            </button>
            <button type="button" className="btn-secondary px-3 py-1 text-xs" onClick={() => setDraft(null)}>
              Cancel
            </button>
          </div>
        </div>
      )}
      {(reindex.error || remove.error || open.error || save.error) && (
        <p className="mt-2 text-xs text-danger">{errorText(reindex.error ?? remove.error ?? open.error ?? save.error)}</p>
      )}
    </li>
  )
}

function AddSource({ onAdded }: { onAdded: () => void }) {
  const [mode, setMode] = useState<'path' | 'text'>('path')
  const [path, setPath] = useState('')
  const [name, setName] = useState('')
  const [filename, setFilename] = useState('')
  const [text, setText] = useState('')
  const [binary, setBinary] = useState<string | null>(null)
  const add = useMutation({
    mutationFn: () =>
      mode === 'path'
        ? api.createKnowledge({ kind: 'path', path, name: name || undefined })
        : api.createKnowledge({
            kind: 'text',
            filename: filename || 'pasted.txt',
            text,
            content_base64: binary ?? undefined,
            name: name || undefined,
          }),
    onSuccess: () => {
      setPath('')
      setName('')
      setFilename('')
      setText('')
      setBinary(null)
      onAdded()
    },
  })
  return (
    <div className="card space-y-3">
      <h2 className="section-title">Connect knowledge</h2>
      <div className="flex gap-1.5">
        <button type="button" className={mode === 'path' ? 'btn-primary px-3 py-1 text-xs' : 'btn-secondary px-3 py-1 text-xs'} onClick={() => setMode('path')}>
          File or folder
        </button>
        <button type="button" className={mode === 'text' ? 'btn-primary px-3 py-1 text-xs' : 'btn-secondary px-3 py-1 text-xs'} onClick={() => setMode('text')}>
          Upload or paste
        </button>
      </div>
      {mode === 'path' ? (
        <label className="block space-y-1">
          <span className="text-sm text-ink">Path on this computer</span>
          <input className="field w-full font-mono text-xs" value={path} onChange={(e) => setPath(e.target.value)} placeholder="~/Documents/inventory.csv" />
          <span className="block text-xs text-ink-faint">Yggdrasil reads it in place and picks up edits automatically.</span>
        </label>
      ) : (
        <>
          <label className="btn-secondary inline-block cursor-pointer px-3 py-1.5 text-xs">
            Choose a file
            <input
              type="file"
              accept={UPLOAD_ACCEPT}
              className="sr-only"
              onChange={async (e) => {
                const f = e.target.files?.[0]
                if (!f) return
                const upload = await readUpload(f)
                setFilename(upload.filename)
                setText(upload.text ?? '')
                setBinary(upload.contentBase64 ?? null)
              }}
            />
          </label>
          {filename && <span className="ml-2 text-xs text-ink-muted">{filename}</span>}
          <textarea className="field min-h-32 w-full font-mono text-xs" value={text} onChange={(e) => setText(e.target.value)} placeholder="Paste text, Markdown, or CSV" aria-label="Content" />
        </>
      )}
      <label className="block space-y-1">
        <span className="text-sm text-ink">Name (optional)</span>
        <input className="field w-full" value={name} onChange={(e) => setName(e.target.value)} />
      </label>
      {add.error && <p className="text-sm text-danger">{errorText(add.error)}</p>}
      {add.data?.status === 'failed' && <p className="text-sm text-danger">{add.data.error}</p>}
      <button
        type="button"
        className="btn-primary px-3 py-1.5 text-sm"
        disabled={add.isPending || (mode === 'path' ? !path.trim() : !text.trim() && !binary)}
        onClick={() => add.mutate()}
      >
        {add.isPending ? 'Indexing…' : 'Connect'}
      </button>
    </div>
  )
}

function SearchBox() {
  const [query, setQuery] = useState('')
  const search = useMutation({ mutationFn: () => api.searchKnowledge(query) })
  return (
    <div className="card space-y-3">
      <h2 className="section-title">Try a question</h2>
      <p className="text-sm text-ink-muted">See which passages a question would bring into the chat.</p>
      <form
        className="flex gap-2"
        onSubmit={(e) => {
          e.preventDefault()
          if (query.trim()) search.mutate()
        }}
      >
        <input className="field flex-1" value={query} onChange={(e) => setQuery(e.target.value)} placeholder="Do you have 225/45R17 in stock?" aria-label="Question" />
        <button type="submit" className="btn-secondary px-3 py-1.5 text-sm" disabled={search.isPending}>
          Search
        </button>
      </form>
      {search.data && search.data.length === 0 && <p className="text-sm text-ink-muted">Nothing matched.</p>}
      <ul className="space-y-2">
        {(search.data ?? []).map((h, i) => (
          <li key={i} className="rounded-lg bg-raised p-3 text-sm">
            <p className="text-xs text-mimir">{h.title}</p>
            <p className="mt-1 whitespace-pre-wrap text-ink-muted">{h.body}</p>
          </li>
        ))}
      </ul>
    </div>
  )
}
