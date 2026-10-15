import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '@/lib/api'
import type { APIKeyPermissions, APIKeyRecord } from '@/types/api'
import { ShownError, shownOr, type Shown } from '@/components/ui/ShownError'

const DEFAULTS: APIKeyPermissions = { memory: 'on_request', knowledge: 'always', tools: 'profile', placement: true }

// Each is apiAccess:permissions.use.<option> in the catalog.
const USE_OPTIONS: APIKeyPermissions['memory'][] = ['never', 'on_request', 'always']

/**
 * What an API key may ask of the assistant (spec §62): memory, connected
 * knowledge, tools, and placement, and the profile it answers with (#345).
 * Requests can narrow it, never widen it.
 */
export function KeyPermissions({ apiKey }: { apiKey: APIKeyRecord }) {
  const { t } = useTranslation('apiAccess')
  const queryClient = useQueryClient()
  const current = apiKey.permissions ?? DEFAULTS
  const [error, setError] = useState<Shown | null>(null)
  const save = useMutation({
    mutationFn: (p: APIKeyPermissions) => api.setApiKeyPermissions(apiKey.id, p),
    onSuccess: () => {
      setError(null)
      void queryClient.invalidateQueries({ queryKey: ['api-keys'] })
    },
    onError: (err) => setError(shownOr(err, t('permissions.saveFailed'))),
  })
  const change = (patch: Partial<APIKeyPermissions>) => save.mutate({ ...current, ...patch })
  const profiles = useQuery({ queryKey: ['profiles'], queryFn: () => api.getProfiles(), retry: false })

  return (
    <details className="w-full text-xs">
      <summary className="cursor-pointer text-ink-muted">{t('permissions.summary')}</summary>
      <div className="mt-2 grid gap-2 sm:grid-cols-2">
        <label className="block sm:col-span-2">
          <span className="text-ink-muted">{t('permissions.profile')}</span>
          <select
            className="field mt-1 w-full"
            value={current.profile ?? ''}
            disabled={save.isPending}
            onChange={(e) => change({ profile: e.target.value || undefined })}
          >
            <option value="">{t('permissions.anyProfile')}</option>
            {(profiles.data ?? []).map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </select>
          {current.profile ? <span className="mt-1 block text-ink-faint">{t('permissions.pinnedHint')}</span> : null}
        </label>
        <label className="block">
          <span className="text-ink-muted">{t('permissions.memories')}</span>
          <select
            className="field mt-1 w-full"
            value={current.memory}
            disabled={save.isPending}
            onChange={(e) => change({ memory: e.target.value as APIKeyPermissions['memory'] })}
          >
            {USE_OPTIONS.map((v) => (
              <option key={v} value={v}>
                {t(`permissions.use.${v}`)}
              </option>
            ))}
          </select>
        </label>
        <label className="block">
          <span className="text-ink-muted">{t('permissions.knowledge')}</span>
          <select
            className="field mt-1 w-full"
            value={current.knowledge}
            disabled={save.isPending}
            onChange={(e) => change({ knowledge: e.target.value as APIKeyPermissions['knowledge'] })}
          >
            {USE_OPTIONS.map((v) => (
              <option key={v} value={v}>
                {t(`permissions.use.${v}`)}
              </option>
            ))}
          </select>
        </label>
        <label className="block">
          <span className="text-ink-muted">{t('permissions.tools')}</span>
          <select
            className="field mt-1 w-full"
            value={current.tools}
            disabled={save.isPending}
            onChange={(e) => change({ tools: e.target.value as APIKeyPermissions['tools'] })}
          >
            <option value="profile">{t('permissions.toolChoices.profile')}</option>
            <option value="read_only">{t('permissions.toolChoices.read_only')}</option>
            <option value="none">{t('permissions.toolChoices.none')}</option>
          </select>
        </label>
        <label className="flex items-center gap-2 pt-5">
          <input
            type="checkbox"
            checked={current.placement}
            disabled={save.isPending}
            onChange={(e) => change({ placement: e.target.checked })}
          />
          <span className="text-ink-muted">{t('permissions.placement')}</span>
        </label>
      </div>
      <p className="mt-2 text-ink-faint">{t('permissions.note')}</p>
      {error && <ShownError {...error} className="mt-1 text-danger" />}
    </details>
  )
}
