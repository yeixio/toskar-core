import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { api } from '@/lib/api'
import type { APIKeyPermissions, APIKeyRecord } from '@/types/api'

const DEFAULTS: APIKeyPermissions = { memory: 'on_request', knowledge: 'always', tools: 'profile', placement: true }

const USE_OPTIONS: [APIKeyPermissions['memory'], string][] = [
  ['never', 'Never'],
  ['on_request', 'When the request asks'],
  ['always', 'Always'],
]

/**
 * What an API key may ask of the assistant (spec §62): memory, connected
 * knowledge, tools, and placement. Requests can narrow it, never widen it.
 */
export function KeyPermissions({ apiKey }: { apiKey: APIKeyRecord }) {
  const queryClient = useQueryClient()
  const current = apiKey.permissions ?? DEFAULTS
  const [error, setError] = useState('')
  const save = useMutation({
    mutationFn: (p: APIKeyPermissions) => api.setApiKeyPermissions(apiKey.id, p),
    onSuccess: () => {
      setError('')
      void queryClient.invalidateQueries({ queryKey: ['api-keys'] })
    },
    onError: (err) => setError(err instanceof Error ? err.message : 'Could not save.'),
  })
  const change = (patch: Partial<APIKeyPermissions>) => save.mutate({ ...current, ...patch })

  return (
    <details className="w-full text-xs">
      <summary className="cursor-pointer text-ink-muted">What this key may use</summary>
      <div className="mt-2 grid gap-2 sm:grid-cols-2">
        <label className="block">
          <span className="text-ink-muted">Your memories</span>
          <select
            className="field mt-1 w-full"
            value={current.memory}
            disabled={save.isPending}
            onChange={(e) => change({ memory: e.target.value as APIKeyPermissions['memory'] })}
          >
            {USE_OPTIONS.map(([v, label]) => (
              <option key={v} value={v}>
                {label}
              </option>
            ))}
          </select>
        </label>
        <label className="block">
          <span className="text-ink-muted">Connected knowledge</span>
          <select
            className="field mt-1 w-full"
            value={current.knowledge}
            disabled={save.isPending}
            onChange={(e) => change({ knowledge: e.target.value as APIKeyPermissions['knowledge'] })}
          >
            {USE_OPTIONS.map(([v, label]) => (
              <option key={v} value={v}>
                {label}
              </option>
            ))}
          </select>
        </label>
        <label className="block">
          <span className="text-ink-muted">Tools</span>
          <select
            className="field mt-1 w-full"
            value={current.tools}
            disabled={save.isPending}
            onChange={(e) => change({ tools: e.target.value as APIKeyPermissions['tools'] })}
          >
            <option value="profile">What the profile allows</option>
            <option value="read_only">Read-only tools</option>
            <option value="none">No tools</option>
          </select>
        </label>
        <label className="flex items-center gap-2 pt-5">
          <input
            type="checkbox"
            checked={current.placement}
            disabled={save.isPending}
            onChange={(e) => change({ placement: e.target.checked })}
          />
          <span className="text-ink-muted">May choose where requests run</span>
        </label>
      </div>
      <p className="mt-2 text-ink-faint">
        Requests can ask for less, never more. Over the network these limits always apply; on this computer they apply
        when the app sends this key.
      </p>
      {error && <p className="mt-1 text-danger">{error}</p>}
    </details>
  )
}
