import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { api } from '@/lib/api'
import type { MCPShare } from '@/types/api'

type AppID = 'claude-desktop' | 'claude-code' | 'cursor' | 'vscode' | 'other'

const APPS: { id: AppID; name: string }[] = [
  { id: 'claude-desktop', name: 'Claude Desktop' },
  { id: 'claude-code', name: 'Claude Code' },
  { id: 'cursor', name: 'Cursor' },
  { id: 'vscode', name: 'VS Code' },
  { id: 'other', name: 'Other apps' },
]

const KEY = '<your API key>'

/** What to paste into each app, and where it goes. */
function setupFor(app: AppID, share: MCPShare): { where: string; text: string; language: string } {
  const auth = share.needs_key ? { Authorization: `Bearer ${KEY}` } : undefined
  const json = (v: unknown) => JSON.stringify(v, null, 2)
  switch (app) {
    case 'claude-desktop': {
      // yggctl dials the default address unless told otherwise.
      const base = share.url.replace(/\/mcp$/, '')
      const env: Record<string, string> = {}
      if (base !== 'http://127.0.0.1:7331') env.YGGDRASIL_URL = base
      if (share.needs_key) env.YGGDRASIL_API_KEY = KEY
      return {
        where: 'In Claude Desktop, open Settings → Developer → Edit Config, add this, and restart Claude.',
        language: 'json',
        text: json({
          mcpServers: {
            yggdrasil: {
              command: share.command,
              args: share.args,
              ...(Object.keys(env).length > 0 ? { env } : {}),
            },
          },
        }),
      }
    }
    case 'claude-code':
      return {
        where: 'Run this in a terminal.',
        language: 'bash',
        text: `claude mcp add --transport http yggdrasil ${share.url}${share.needs_key ? ` --header "Authorization: Bearer ${KEY}"` : ''}`,
      }
    case 'cursor':
      return {
        where: 'Add this to ~/.cursor/mcp.json (Cursor Settings → MCP → Add new global MCP server).',
        language: 'json',
        text: json({ mcpServers: { yggdrasil: { url: share.url, ...(auth ? { headers: auth } : {}) } } }),
      }
    case 'vscode':
      return {
        where: 'Run "MCP: Open User Configuration" from the Command Palette and add this.',
        language: 'json',
        text: json({ servers: { yggdrasil: { type: 'http', url: share.url, ...(auth ? { headers: auth } : {}) } } }),
      }
    default:
      return {
        where:
          'Apps that connect to MCP servers by address use Streamable HTTP at this address. Apps that start a program use the command instead.',
        language: 'text',
        text: `Address: ${share.url}\nCommand: ${share.command} ${share.args.join(' ')}${share.needs_key ? `\nHeader:  Authorization: Bearer ${KEY}` : ''}`,
      }
  }
}

/**
 * Yggdrasil as an MCP server: other AI apps can ask the local AI, list its
 * models, and search connected knowledge, within an API key's permissions.
 */
export function ShareWithApps() {
  const share = useQuery({ queryKey: ['mcp-share'], queryFn: () => api.mcpShare(), retry: false })
  const [app, setApp] = useState<AppID>('claude-desktop')
  const [copied, setCopied] = useState(false)
  if (!share.data) return null
  const setup = setupFor(app, share.data)
  return (
    <section className="card space-y-4" aria-label="Use Yggdrasil in other AI apps">
      <div>
        <h2 className="section-title">Use Yggdrasil in other AI apps</h2>
        <p className="mt-1 max-w-2xl text-sm text-ink-muted">
          Apps that support MCP can ask your local AI, list its models, and search your connected knowledge. Answers stay on
          your computers, and the local AI only uses tools that read.
        </p>
      </div>
      <div role="tablist" className="flex flex-wrap gap-1.5">
        {APPS.map((a) => (
          <button
            key={a.id}
            type="button"
            role="tab"
            aria-selected={app === a.id}
            className={app === a.id ? 'btn-primary px-3 py-1.5 text-xs' : 'btn-secondary px-3 py-1.5 text-xs'}
            onClick={() => {
              setApp(a.id)
              setCopied(false)
            }}
          >
            {a.name}
          </button>
        ))}
      </div>
      <p className="text-sm text-ink-muted">{setup.where}</p>
      <div className="relative">
        <pre className="log-panel overflow-x-auto pr-20 text-xs" aria-label={`${setup.language} settings`}>
          {setup.text}
        </pre>
        <button
          type="button"
          className="btn-secondary absolute right-2 top-2 px-2.5 py-1 text-xs"
          onClick={() => {
            void navigator.clipboard.writeText(setup.text).then(() => setCopied(true))
          }}
        >
          {copied ? 'Copied' : 'Copy'}
        </button>
      </div>
      {share.data.needs_key && (
        <p className="text-xs text-ink-faint">
          Yggdrasil is open to your network, so apps need an API key. Create one below and put it where the settings say{' '}
          <code>{KEY}</code>. The key&apos;s permissions apply.
        </p>
      )}
    </section>
  )
}
