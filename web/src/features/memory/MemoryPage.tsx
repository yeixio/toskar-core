import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { EmptyState } from '@/components/ui/EmptyState'
import { api } from '@/lib/api'
import { subscribeEvents } from '@/lib/events'
import { errorText } from '@/features/train/display'
import type { MemoryCategory, MemoryItem } from '@/types/api'
import { RealmKicker } from '@/components/ui/Realm'

const categoryLabels: Record<MemoryCategory, string> = {
  identity: 'About you',
  preferences: 'Preferences',
  projects: 'Projects',
  technical: 'Technical',
  interests: 'Interests',
  people: 'People',
  other: 'Other',
}

export function MemoryPage() {
  const queryClient = useQueryClient()
  const memory = useQuery({ queryKey: ['memory'], queryFn: () => api.listMemory() })
  const settings = useQuery({ queryKey: ['settings'], queryFn: () => api.getSettings() })
  const refresh = () => void queryClient.invalidateQueries({ queryKey: ['memory'] })

  useEffect(
    () =>
      subscribeEvents({
        onEvent: (e) => {
          if (e.type.startsWith('memory.')) refresh()
        },
      }),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [],
  )

  const toggleAll = useMutation({
    mutationFn: (on: boolean) => api.updateSettings({ memory_enabled: on }),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ['settings'] }),
  })

  const items = memory.data?.memories ?? []
  const categories = memory.data?.categories ?? (Object.keys(categoryLabels) as MemoryCategory[])
  const on = settings.data?.memory_enabled !== false

  return (
    <div className="page-fill gap-4 overflow-y-auto p-4">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <RealmKicker />
          <h1 className="font-display text-2xl font-semibold text-ink">Memory</h1>
          <p className="mt-1 max-w-2xl text-sm text-ink-muted">
            Things you asked Yggdrasil to remember. They belong to Yggdrasil, not to one model, so they carry across
            chats, restarts, and model changes. Only the memories that fit a question are used, and they stay on this
            computer. Say “Remember that …” in any chat to add one.
          </p>
        </div>
        <label className="flex items-center gap-2 text-sm text-ink">
          <input type="checkbox" checked={on} disabled={toggleAll.isPending} onChange={(e) => toggleAll.mutate(e.target.checked)} />
          Use memory in chats
        </label>
      </div>
      {!on && (
        <p className="rounded-md bg-warning/10 p-3 text-sm text-warning">
          Memory is off. Chats do not use what is saved here, and nothing is deleted.
        </p>
      )}
      <AddMemory categories={categories} onAdded={refresh} />
      {memory.isLoading && <p className="text-sm text-ink-muted">Loading…</p>}
      {!memory.isLoading && items.length === 0 && (
        <EmptyState
          title="Nothing remembered yet"
          description="Tell Yggdrasil things worth keeping, such as “Remember that I prefer metric units” or “Remember that this project uses Go.”"
        />
      )}
      {categories.map((cat) => {
        const list = items.filter((m) => m.category === cat)
        if (list.length === 0) return null
        return (
          <section key={cat} className="space-y-2">
            <h2 className="label-caps">{categoryLabels[cat] ?? cat}</h2>
            <ul className="space-y-2">
              {list.map((m) => (
                <MemoryRow key={m.id} memory={m} categories={categories} onChanged={refresh} />
              ))}
            </ul>
          </section>
        )
      })}
    </div>
  )
}

function AddMemory({ categories, onAdded }: { categories: MemoryCategory[]; onAdded: () => void }) {
  const [content, setContent] = useState('')
  const [category, setCategory] = useState<MemoryCategory | ''>('')
  const add = useMutation({
    mutationFn: () => api.addMemory(content, category || undefined),
    onSuccess: () => {
      setContent('')
      setCategory('')
      onAdded()
    },
  })
  return (
    <form
      className="card flex flex-wrap items-end gap-2 !p-4"
      onSubmit={(e) => {
        e.preventDefault()
        if (content.trim()) add.mutate()
      }}
    >
      <label className="min-w-0 flex-1 space-y-1">
        <span className="text-sm text-ink">Add a memory</span>
        <input className="field w-full" value={content} onChange={(e) => setContent(e.target.value)} placeholder="I prefer short answers with examples" />
      </label>
      <select className="field" value={category} onChange={(e) => setCategory(e.target.value as MemoryCategory)} aria-label="Category">
        <option value="">Choose for me</option>
        {categories.map((c) => (
          <option key={c} value={c}>
            {categoryLabels[c] ?? c}
          </option>
        ))}
      </select>
      <button type="submit" className="btn-primary px-3 py-1.5 text-sm" disabled={!content.trim() || add.isPending}>
        Add
      </button>
      {add.error && <p className="w-full text-sm text-danger">{errorText(add.error)}</p>}
    </form>
  )
}

function MemoryRow({ memory, categories, onChanged }: { memory: MemoryItem; categories: MemoryCategory[]; onChanged: () => void }) {
  const [draft, setDraft] = useState<string | null>(null)
  const update = useMutation({
    mutationFn: (body: { content?: string; category?: MemoryCategory; enabled?: boolean }) => api.updateMemory(memory.id, body),
    onSuccess: () => {
      setDraft(null)
      onChanged()
    },
  })
  const remove = useMutation({ mutationFn: () => api.deleteMemory(memory.id), onSuccess: onChanged })
  return (
    <li className={['card !p-3', memory.enabled ? '' : 'opacity-60'].join(' ')}>
      {draft != null ? (
        <form
          className="flex gap-2"
          onSubmit={(e) => {
            e.preventDefault()
            update.mutate({ content: draft })
          }}
        >
          <input className="field flex-1" value={draft} onChange={(e) => setDraft(e.target.value)} aria-label="Memory text" autoFocus />
          <button type="submit" className="btn-primary px-3 py-1 text-xs" disabled={update.isPending}>
            Save
          </button>
          <button type="button" className="btn-secondary px-3 py-1 text-xs" onClick={() => setDraft(null)}>
            Cancel
          </button>
        </form>
      ) : (
        <div className="flex flex-wrap items-center gap-2">
          <p className="min-w-[12rem] flex-1 text-sm text-ink">{memory.content}</p>
          <span className="text-xs text-ink-faint">
            {memory.source_type === 'explicit' ? 'You asked in a chat' : 'Added here'} ·{' '}
            {new Date(memory.updated_at).toLocaleDateString()}
          </span>
          <select
            className="field py-0.5 text-xs"
            value={memory.category}
            aria-label="Category"
            onChange={(e) => update.mutate({ category: e.target.value as MemoryCategory })}
          >
            {categories.map((c) => (
              <option key={c} value={c}>
                {categoryLabels[c] ?? c}
              </option>
            ))}
          </select>
          <button type="button" className="btn-secondary px-2 py-0.5 text-xs" onClick={() => setDraft(memory.content)}>
            Edit
          </button>
          <button type="button" className="btn-secondary px-2 py-0.5 text-xs" disabled={update.isPending} onClick={() => update.mutate({ enabled: !memory.enabled })}>
            {memory.enabled ? 'Pause' : 'Use again'}
          </button>
          <button type="button" className="btn-secondary px-2 py-0.5 text-xs" disabled={remove.isPending} onClick={() => remove.mutate()} aria-label={`Delete memory: ${memory.content}`}>
            Delete
          </button>
        </div>
      )}
      {(update.error || remove.error) && <p className="mt-1 text-xs text-danger">{errorText(update.error ?? remove.error)}</p>}
    </li>
  )
}
