import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useMemo, useState, type ReactNode } from 'react'
import { api } from '@/lib/api'
import type {
  MCPAddRequest,
  MCPAdded,
  MCPField,
  MCPGalleryEntry,
  MCPImportCandidate,
  MCPNeed,
  MCPParsed,
  MCPSpec,
} from '@/types/api'
import { preopenSignInWindow } from '@/lib/desktopBridge'
import { errorText, linesToMap, openSignIn, SOURCES_KEY, specSummary, splitArgs } from './mcpShared'

const TABS = [
  { id: 'gallery', label: 'Gallery' },
  { id: 'paste', label: 'Paste' },
  { id: 'apps', label: 'From your other apps' },
  { id: 'custom', label: 'Custom' },
] as const

type TabId = (typeof TABS)[number]['id']

/**
 * Adding a tool source, four ways: pick one from the gallery and answer a
 * question or two, paste whatever a server's instructions say, bring the
 * servers set up in other apps on this computer, or fill in the form.
 */
export function AddToolSource({ onClose }: { onClose: () => void }) {
  const [tab, setTab] = useState<TabId>('gallery')
  const [done, setDone] = useState<MCPAdded | null>(null)
  return (
    <section className="card space-y-4" aria-label="Add tools">
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div>
          <h2 className="section-title">Add tools</h2>
          <p className="mt-1 max-w-2xl text-sm text-ink-muted">
            Tool sources use the Model Context Protocol (MCP), the standard most AI apps share. Their tools work in every
            chat and automation. Reading is allowed; anything that changes something asks you first.
          </p>
        </div>
        <button type="button" className="btn-secondary px-3 py-1.5 text-xs" onClick={onClose}>
          Close
        </button>
      </div>
      {done ? (
        <AddedNotice added={done} onAnother={() => setDone(null)} onClose={onClose} />
      ) : (
        <>
          <div role="tablist" className="flex flex-wrap gap-1.5">
            {TABS.map((t) => (
              <button
                key={t.id}
                type="button"
                role="tab"
                aria-selected={tab === t.id}
                className={tab === t.id ? 'btn-primary px-3 py-1.5 text-xs' : 'btn-secondary px-3 py-1.5 text-xs'}
                onClick={() => setTab(t.id)}
              >
                {t.label}
              </button>
            ))}
          </div>
          {tab === 'gallery' && <GalleryTab onAdded={setDone} />}
          {tab === 'paste' && <PasteTab onAdded={setDone} />}
          {tab === 'apps' && <AppsTab onAdded={setDone} />}
          {tab === 'custom' && <CustomTab onAdded={setDone} />}
        </>
      )}
    </section>
  )
}

/** useAdd adds a source and, when the service needs a sign-in, opens it. */
function useAdd(onAdded: (added: MCPAdded) => void) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async ({ body, popup }: { body: MCPAddRequest; popup?: Window | null }) => {
      try {
        const added = await api.addMCPServer(body)
        if (added.sign_in_url) openSignIn(added.sign_in_url, popup)
        else popup?.close()
        return added
      } catch (err) {
        popup?.close()
        throw err
      }
    },
    onSuccess: (added) => {
      void queryClient.invalidateQueries({ queryKey: SOURCES_KEY })
      void queryClient.invalidateQueries({ queryKey: ['tools'] })
      void queryClient.invalidateQueries({ queryKey: ['mcp-gallery'] })
      void queryClient.invalidateQueries({ queryKey: ['mcp-import'] })
      onAdded(added)
    },
  })
}

/** Work is the message shown while a source is being added. */
function Working({ local }: { local: boolean }) {
  return (
    <p className="rounded-md bg-info/10 px-2.5 py-2 text-xs leading-relaxed text-ink" role="status">
      {local
        ? 'Starting it and listing its tools. The first start downloads it, which can take a minute.'
        : 'Connecting and listing its tools…'}
    </p>
  )
}

function AddedNotice({ added, onAnother, onClose }: { added: MCPAdded; onAnother: () => void; onClose: () => void }) {
  const s = added.server
  const reads = s.tools.filter((t) => t.risk === 'read').length
  const changes = s.tools.length - reads
  return (
    <div className="space-y-3" role="status">
      {added.sign_in_url ? (
        <>
          <p className="text-sm text-ink">
            <span className="font-semibold">{s.name}</span> needs you to sign in. Finish in the window that opened; its
            tools appear here when you are done.
          </p>
          <a className="btn-primary px-3 py-1.5 text-xs" href={added.sign_in_url} target="_blank" rel="noreferrer">
            Open the sign-in again
          </a>
        </>
      ) : (
        <p className="text-sm text-ink">
          <span className="font-semibold">{s.name}</span> is ready with {s.tools.length} tool
          {s.tools.length === 1 ? '' : 's'}
          {s.tools.length > 0 && (
            <>
              : {reads} that read{changes > 0 ? `, and ${changes} that change things and ask you first` : ''}
            </>
          )}
          . Ask about it in chat.
        </p>
      )}
      <div className="flex gap-2">
        <button type="button" className="btn-secondary px-3 py-1.5 text-xs" onClick={onAnother}>
          Add another
        </button>
        <button type="button" className="btn-secondary px-3 py-1.5 text-xs" onClick={onClose}>
          Done
        </button>
      </div>
    </div>
  )
}

function GalleryTab({ onAdded }: { onAdded: (a: MCPAdded) => void }) {
  const gallery = useQuery({ queryKey: ['mcp-gallery'], queryFn: () => api.mcpGallery(), retry: false })
  const [chosen, setChosen] = useState<MCPGalleryEntry | null>(null)
  const groups = useMemo(() => {
    const out = new Map<string, MCPGalleryEntry[]>()
    for (const e of gallery.data ?? []) {
      out.set(e.category, [...(out.get(e.category) ?? []), e])
    }
    return [...out.entries()]
  }, [gallery.data])

  if (chosen) return <GallerySetup entry={chosen} onBack={() => setChosen(null)} onAdded={onAdded} />
  if (gallery.isError) return <p className="text-sm text-danger">{errorText(gallery.error)}</p>
  return (
    <div className="space-y-4">
      {gallery.isLoading && <p className="text-sm text-ink-muted">Loading the gallery…</p>}
      {groups.map(([category, entries]) => (
        <div key={category} className="space-y-2">
          <p className="label-caps">{category}</p>
          <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-3">
            {entries.map((e) => (
              <button key={e.id} type="button" className="selectable space-y-1.5" onClick={() => setChosen(e)}>
                <span className="flex flex-wrap items-center gap-1.5">
                  <span className="selectable-title text-sm font-semibold text-ink">{e.name}</span>
                  {e.added && <span className="status-chip bg-success/15 text-success">Added</span>}
                  {e.sign_in && <span className="status-chip bg-raised text-ink-muted">Sign in</span>}
                  {e.missing && <span className="status-chip bg-warning/15 text-warning">Needs {e.missing}</span>}
                </span>
                <span className="block text-xs text-ink-muted">{e.description}</span>
                <span className="block text-[11px] text-ink-faint">{e.remote ? 'Runs on the web' : 'Runs on this computer'}</span>
              </button>
            ))}
          </div>
        </div>
      ))}
    </div>
  )
}

function GallerySetup({ entry, onBack, onAdded }: { entry: MCPGalleryEntry; onBack: () => void; onAdded: (a: MCPAdded) => void }) {
  const [values, setValues] = useState<Record<string, string>>(() =>
    Object.fromEntries(entry.fields.map((f) => [f.key, f.default ?? ''])),
  )
  const add = useAdd(onAdded)
  return (
    <form
      className="space-y-3"
      onSubmit={(ev) => {
        ev.preventDefault()
        // Open the sign-in window now, while the click still counts, so the
        // browser does not block it.
        const popup = entry.sign_in ? preopenSignInWindow() : null
        add.mutate({ body: { preset: entry.id, values }, popup })
      }}
    >
      <div>
        <button type="button" className="text-xs text-ink-muted hover:text-ink" onClick={onBack}>
          ← Gallery
        </button>
        <h3 className="mt-1 font-display text-lg font-semibold text-ink">{entry.name}</h3>
        <p className="text-sm text-ink-muted">{entry.description}</p>
        {entry.homepage && (
          <a className="text-xs text-primary hover:underline" href={entry.homepage} target="_blank" rel="noreferrer">
            About this server
          </a>
        )}
      </div>
      {entry.missing && <MissingRuntime name={entry.missing} />}
      {entry.setup && <p className="rounded-md bg-info/10 px-2.5 py-2 text-xs leading-relaxed text-ink">{entry.setup}</p>}
      {entry.sign_in && (
        <p className="text-xs text-ink-muted">
          A window opens for you to sign in to {entry.name} and choose what Yggdrasil may use. Your sign-in stays on this
          computer.
        </p>
      )}
      {entry.fields.map((f) => (
        <FieldInput key={f.key} field={f} value={values[f.key] ?? ''} onChange={(v) => setValues((cur) => ({ ...cur, [f.key]: v }))} />
      ))}
      {add.isPending && <Working local={!entry.remote} />}
      {add.isError && <p className="text-xs text-danger">{errorText(add.error)}</p>}
      <button type="submit" className="btn-primary px-3 py-1.5 text-xs" disabled={add.isPending}>
        {add.isPending ? 'Adding…' : entry.sign_in ? `Sign in and add ${entry.name}` : `Add ${entry.name}`}
      </button>
    </form>
  )
}

function FieldInput({ field, value, onChange }: { field: MCPField; value: string; onChange: (v: string) => void }) {
  const label = (
    <span className="text-ink-muted">
      {field.label}
      {field.optional && <span className="text-ink-faint"> (optional)</span>}
    </span>
  )
  return (
    <label className="block text-sm">
      {label}
      {field.kind === 'folders' ? (
        <textarea
          className="field mt-1 min-h-20 w-full font-mono text-xs"
          value={value}
          spellCheck={false}
          placeholder={field.placeholder}
          onChange={(e) => onChange(e.target.value)}
        />
      ) : (
        <input
          className="field mt-1 w-full"
          type={field.secret ? 'password' : 'text'}
          autoComplete="off"
          spellCheck={false}
          value={value}
          placeholder={field.placeholder}
          onChange={(e) => onChange(e.target.value)}
        />
      )}
      {field.help && <span className="mt-1 block text-xs text-ink-faint">{field.help}</span>}
    </label>
  )
}

function MissingRuntime({ name }: { name: string }) {
  const help: Record<string, ReactNode> = {
    'Node.js': (
      <>
        Install Node.js from{' '}
        <a className="underline" href="https://nodejs.org" target="_blank" rel="noreferrer">
          nodejs.org
        </a>{' '}
        (or run <code>brew install node</code>), then add it.
      </>
    ),
    uv: (
      <>
        Install uv from{' '}
        <a className="underline" href="https://docs.astral.sh/uv/" target="_blank" rel="noreferrer">
          docs.astral.sh/uv
        </a>{' '}
        (or run <code>brew install uv</code>), then add it.
      </>
    ),
    Docker: (
      <>
        Install and start{' '}
        <a className="underline" href="https://www.docker.com/products/docker-desktop/" target="_blank" rel="noreferrer">
          Docker Desktop
        </a>
        , then add it.
      </>
    ),
  }
  return (
    <p className="rounded-md bg-warning/10 px-2.5 py-2 text-xs leading-relaxed text-ink">
      This runs on this computer and needs {name}, which is not installed. {help[name] ?? `Install ${name}, then add it.`}
    </p>
  )
}

function PasteTab({ onAdded }: { onAdded: (a: MCPAdded) => void }) {
  const [text, setText] = useState('')
  const parse = useMutation({ mutationFn: () => api.parseMCP(text) })
  return (
    <div className="space-y-3">
      <label className="block text-sm">
        <span className="text-ink-muted">
          Paste what a server&apos;s instructions say: its JSON settings for any app, its web address, or the command that
          runs it.
        </span>
        <textarea
          className="field mt-1 min-h-32 w-full font-mono text-xs"
          value={text}
          spellCheck={false}
          placeholder={'{\n  "mcpServers": {\n    "example": { "command": "npx", "args": ["-y", "example-mcp"] }\n  }\n}\n\nor  https://mcp.example.com/mcp\nor  npx -y example-mcp'}
          onChange={(e) => {
            setText(e.target.value)
            parse.reset()
          }}
        />
      </label>
      <button
        type="button"
        className="btn-primary px-3 py-1.5 text-xs"
        disabled={!text.trim() || parse.isPending}
        onClick={() => parse.mutate()}
      >
        {parse.isPending ? 'Reading…' : 'Read it'}
      </button>
      {parse.isError && <p className="text-xs text-danger">{errorText(parse.error)}</p>}
      {parse.data?.map((p, i) => <ParsedServer key={`${p.spec.name}-${i}`} parsed={p} onAdded={onAdded} />)}
    </div>
  )
}

/** One server read from pasted text, with what it still needs. */
function ParsedServer({ parsed, onAdded }: { parsed: MCPParsed; onAdded: (a: MCPAdded) => void }) {
  const [name, setName] = useState(parsed.spec.name)
  const [values, setValues] = useState<Record<string, string>>({})
  const add = useAdd(onAdded)
  const local = !parsed.spec.url
  const missingValues = parsed.needs.some((n) => !values[n.key]?.trim())
  return (
    <form
      className="space-y-2 rounded-lg border border-line/60 p-3"
      onSubmit={(e) => {
        e.preventDefault()
        add.mutate({ body: { spec: { ...parsed.spec, name }, values } })
      }}
    >
      <label className="block text-sm">
        <span className="text-ink-muted">Name</span>
        <input className="field mt-1 w-full" value={name} onChange={(e) => setName(e.target.value)} />
      </label>
      <p className="break-anywhere font-mono text-[11px] text-ink-faint">{specSummary(parsed.spec)}</p>
      {parsed.missing && <MissingRuntime name={parsed.missing} />}
      <NeedsInputs needs={parsed.needs} values={values} setValues={setValues} />
      {add.isPending && <Working local={local} />}
      {add.isError && <p className="text-xs text-danger">{errorText(add.error)}</p>}
      <button type="submit" className="btn-primary px-3 py-1.5 text-xs" disabled={add.isPending || missingValues || !name.trim()}>
        {add.isPending ? 'Adding…' : `Add ${name || 'it'}`}
      </button>
    </form>
  )
}

function NeedsInputs({
  needs,
  values,
  setValues,
}: {
  needs: MCPNeed[]
  values: Record<string, string>
  setValues: (fn: (cur: Record<string, string>) => Record<string, string>) => void
}) {
  if (needs.length === 0) return null
  return (
    <div className="space-y-2">
      <p className="text-xs text-ink-muted">Fill in what the instructions left for you to add:</p>
      {needs.map((n) => (
        <label key={n.key} className="block text-sm">
          <span className="font-mono text-xs text-ink-muted">{n.label}</span>
          <input
            className="field mt-1 w-full"
            type={n.secret ? 'password' : 'text'}
            autoComplete="off"
            spellCheck={false}
            value={values[n.key] ?? ''}
            onChange={(e) => setValues((cur) => ({ ...cur, [n.key]: e.target.value }))}
          />
        </label>
      ))}
      {needs.some((n) => n.secret) && (
        <p className="text-[11px] text-ink-faint">Secrets are kept on this computer, outside the AI&apos;s view.</p>
      )}
    </div>
  )
}

function AppsTab({ onAdded }: { onAdded: (a: MCPAdded) => void }) {
  const found = useQuery({ queryKey: ['mcp-import'], queryFn: () => api.mcpImportCandidates(), retry: false })
  const groups = useMemo(() => {
    const out = new Map<string, MCPImportCandidate[]>()
    for (const c of found.data ?? []) out.set(c.app_name, [...(out.get(c.app_name) ?? []), c])
    return [...out.entries()]
  }, [found.data])
  if (found.isLoading) return <p className="text-sm text-ink-muted">Looking at your other apps…</p>
  if (found.isError) return <p className="text-sm text-danger">{errorText(found.error)}</p>
  if (groups.length === 0) {
    return (
      <p className="text-sm text-ink-muted">
        No MCP servers were found in Claude Desktop, Claude Code, Cursor, VS Code, Windsurf, Gemini CLI, or LM Studio on
        this computer.
      </p>
    )
  }
  return (
    <div className="space-y-4">
      <p className="text-sm text-ink-muted">
        Servers you already set up in other apps. Adding one copies its settings; its secrets go straight to Yggdrasil&apos;s
        secret store.
      </p>
      {groups.map(([app, list]) => (
        <div key={app} className="space-y-2">
          <p className="label-caps">{app}</p>
          {list.map((c) => (
            <ImportRow key={`${c.app}-${c.spec.name}`} candidate={c} onAdded={onAdded} />
          ))}
        </div>
      ))}
    </div>
  )
}

function ImportRow({ candidate, onAdded }: { candidate: MCPImportCandidate; onAdded: (a: MCPAdded) => void }) {
  const [values, setValues] = useState<Record<string, string>>({})
  const add = useAdd(onAdded)
  const needs = candidate.needs ?? []
  return (
    <div className="space-y-2 rounded-lg border border-line/60 p-3">
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div className="min-w-0">
          <p className="text-sm font-medium text-ink">{candidate.spec.name}</p>
          <p className="break-anywhere font-mono text-[11px] text-ink-faint">{specSummary(candidate.spec)}</p>
        </div>
        {candidate.added ? (
          <span className="status-chip bg-success/15 text-success">Added</span>
        ) : (
          <button
            type="button"
            className="btn-primary shrink-0 px-3 py-1.5 text-xs"
            disabled={add.isPending || needs.some((n) => !values[n.key]?.trim())}
            onClick={() =>
              add.mutate({ body: { import: { app: candidate.app, name: candidate.spec.name }, values } })
            }
          >
            {add.isPending ? 'Adding…' : 'Add'}
          </button>
        )}
      </div>
      {!candidate.added && candidate.missing && <MissingRuntime name={candidate.missing} />}
      {!candidate.added && <NeedsInputs needs={needs} values={values} setValues={setValues} />}
      {add.isPending && <Working local={!candidate.spec.url} />}
      {add.isError && <p className="text-xs text-danger">{errorText(add.error)}</p>}
    </div>
  )
}

function CustomTab({ onAdded }: { onAdded: (a: MCPAdded) => void }) {
  const [where, setWhere] = useState<'local' | 'remote'>('local')
  const [name, setName] = useState('')
  const [command, setCommand] = useState('')
  const [env, setEnv] = useState('')
  const [url, setURL] = useState('')
  const [headers, setHeaders] = useState('')
  const [clientID, setClientID] = useState('')
  const add = useAdd(onAdded)
  const ready = name.trim() && (where === 'local' ? command.trim() : url.trim())
  return (
    <form
      className="space-y-3"
      onSubmit={(e) => {
        e.preventDefault()
        let spec: MCPSpec
        if (where === 'local') {
          const [cmd, ...args] = splitArgs(command)
          spec = { name: name.trim(), command: cmd, args, env: linesToMap(env, '=') }
        } else {
          spec = { name: name.trim(), url: url.trim(), headers: linesToMap(headers, ':'), client_id: clientID.trim() || undefined }
        }
        add.mutate({ body: { spec } })
      }}
    >
      <div className="flex gap-1.5" role="radiogroup" aria-label="Where it runs">
        {(['local', 'remote'] as const).map((w) => (
          <button
            key={w}
            type="button"
            role="radio"
            aria-checked={where === w}
            className={where === w ? 'btn-primary px-3 py-1.5 text-xs' : 'btn-secondary px-3 py-1.5 text-xs'}
            onClick={() => setWhere(w)}
          >
            {w === 'local' ? 'Runs on this computer' : 'On the web'}
          </button>
        ))}
      </div>
      <label className="block text-sm">
        <span className="text-ink-muted">Name</span>
        <input className="field mt-1 w-full" value={name} placeholder="My tools" onChange={(e) => setName(e.target.value)} />
      </label>
      {where === 'local' ? (
        <>
          <label className="block text-sm">
            <span className="text-ink-muted">Command</span>
            <input
              className="field mt-1 w-full font-mono text-xs"
              value={command}
              spellCheck={false}
              placeholder="npx -y @scope/some-mcp-server --flag"
              onChange={(e) => setCommand(e.target.value)}
            />
          </label>
          <label className="block text-sm">
            <span className="text-ink-muted">
              Environment variables <span className="text-ink-faint">(optional, one KEY=value per line)</span>
            </span>
            <textarea
              className="field mt-1 min-h-16 w-full font-mono text-xs"
              value={env}
              spellCheck={false}
              placeholder="API_KEY=…"
              onChange={(e) => setEnv(e.target.value)}
            />
          </label>
        </>
      ) : (
        <>
          <label className="block text-sm">
            <span className="text-ink-muted">Address</span>
            <input
              className="field mt-1 w-full font-mono text-xs"
              value={url}
              spellCheck={false}
              placeholder="https://mcp.example.com/mcp"
              onChange={(e) => setURL(e.target.value)}
            />
          </label>
          <label className="block text-sm">
            <span className="text-ink-muted">
              Headers <span className="text-ink-faint">(optional, one Name: value per line)</span>
            </span>
            <textarea
              className="field mt-1 min-h-16 w-full font-mono text-xs"
              value={headers}
              spellCheck={false}
              placeholder="Authorization: Bearer …"
              onChange={(e) => setHeaders(e.target.value)}
            />
          </label>
          <label className="block text-sm">
            <span className="text-ink-muted">
              Sign-in client ID <span className="text-ink-faint">(only if the service gave you one)</span>
            </span>
            <input className="field mt-1 w-full" value={clientID} onChange={(e) => setClientID(e.target.value)} />
          </label>
        </>
      )}
      <p className="text-[11px] text-ink-faint">Keys, tokens, passwords, and headers are kept on this computer, outside the AI&apos;s view.</p>
      {add.isPending && <Working local={where === 'local'} />}
      {add.isError && <p className="text-xs text-danger">{errorText(add.error)}</p>}
      <button type="submit" className="btn-primary px-3 py-1.5 text-xs" disabled={!ready || add.isPending}>
        {add.isPending ? 'Adding…' : 'Add'}
      </button>
    </form>
  )
}
