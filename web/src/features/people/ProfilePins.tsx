import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '@/lib/api'
import type { AIProfile, Person } from '@/types/api'
import { ShownError, shownError, type Shown } from '@/components/ui/ShownError'
import { LoadError } from '@/components/ui/LoadError'

/**
 * Pinning profiles (#345): Admins keep Members and Visitors, or one
 * person, to one or more profiles, such as an assistant with topic
 * controls. The Owner and Admins always have every profile.
 */

function useProfiles() {
  return useQuery({ queryKey: ['profiles'], queryFn: () => api.getProfiles(), retry: false })
}

/** The profiles' names, or "Any profile" for none. */
function namesOf(ids: string[], profiles: AIProfile[] | null | undefined, any: string): string {
  if (ids.length === 0) return any
  return ids.map((id) => profiles?.find((p) => p.id === id)?.name ?? id).join(', ')
}

function Checklist({
  value,
  onChange,
  disabled,
  label,
}: {
  value: string[]
  onChange: (ids: string[]) => void
  disabled?: boolean
  label: string
}) {
  const profiles = useProfiles()
  return (
    <fieldset className="grid gap-1 sm:grid-cols-2" disabled={disabled} aria-label={label}>
      {(profiles.data ?? []).map((p) => (
        <label key={p.id} className="flex items-center gap-2 text-sm">
          <input
            type="checkbox"
            checked={value.includes(p.id)}
            onChange={(e) => onChange(e.target.checked ? [...value, p.id] : value.filter((id) => id !== p.id))}
          />
          <span className="truncate">{p.name}</span>
        </label>
      ))}
    </fieldset>
  )
}

/** The section that pins Members and Visitors to profiles. */
export function RolePins() {
  const { t } = useTranslation('people')
  const queryClient = useQueryClient()
  const profiles = useProfiles()
  const pins = useQuery({ queryKey: ['profile-pins'], queryFn: () => api.getProfilePins(), retry: false })
  const [error, setError] = useState<Shown | null>(null)
  const save = useMutation({
    mutationFn: ({ role, ids }: { role: 'member' | 'visitor'; ids: string[] }) => api.setRoleProfiles(role, ids),
    onSuccess: (next) => {
      setError(null)
      if (next) queryClient.setQueryData(['profile-pins'], next)
    },
    onError: (err) => setError(shownError(err)),
  })
  // Not read: an Admin sees why; an older computer without pins has no section.
  if (!pins.data) {
    if (!pins.isError) return null
    return (
      <section className="card space-y-3" aria-labelledby="profile-pins">
        <h2 id="profile-pins" className="section-title">
          {t('pins.title')}
        </h2>
        <LoadError compact error={pins.error} onRetry={() => void pins.refetch()} retrying={pins.isFetching} />
      </section>
    )
  }
  return (
    <section className="card space-y-3" aria-labelledby="profile-pins">
      <h2 id="profile-pins" className="section-title">
        {t('pins.title')}
      </h2>
      <p className="text-sm text-ink-muted">{t('pins.body')}</p>
      {(['member', 'visitor'] as const).map((role) => {
        const ids = pins.data?.roles[role] ?? []
        return (
          <details key={role} className="rounded-lg border border-line p-3">
            <summary className="cursor-pointer text-sm">
              <span className="font-medium text-ink">{t(`pins.${role}s`)}</span>
              <span className="ms-2 text-ink-muted">{namesOf(ids, profiles.data, t('pins.any'))}</span>
            </summary>
            <div className="mt-3 space-y-2">
              <Checklist
                value={ids}
                label={t(`pins.${role}s`)}
                disabled={save.isPending}
                onChange={(next) => save.mutate({ role, ids: next })}
              />
              <p className="text-xs text-ink-faint">{t('pins.noneTicked')}</p>
            </div>
          </details>
        )
      })}
      {error ? <ShownError {...error} className="text-sm text-danger" /> : null}
    </section>
  )
}

type Mode = 'role' | 'any' | 'only'

/** One person's pin: their role's, any profile, or only some. */
export function PersonPins({ person }: { person: Person }) {
  const { t } = useTranslation('people')
  const queryClient = useQueryClient()
  const profiles = useProfiles()
  const mode: Mode = person.profiles === undefined ? 'role' : person.profiles.length === 0 ? 'any' : 'only'
  const [picking, setPicking] = useState(false)
  const [error, setError] = useState<Shown | null>(null)
  const save = useMutation({
    mutationFn: (ids: string[] | null) => api.setPersonProfiles(person.id, ids),
    onSuccess: () => {
      setError(null)
      void queryClient.invalidateQueries({ queryKey: ['people'] })
    },
    onError: (err) => setError(shownError(err)),
  })
  const shown: Mode = picking ? 'only' : mode
  const summary =
    mode === 'role' ? t('pins.sameAsRole') : namesOf(person.profiles ?? [], profiles.data, t('pins.any'))
  return (
    <details className="w-full text-xs">
      <summary className="cursor-pointer text-ink-muted">
        {t('pins.profiles')}: <span className="text-ink">{summary}</span>
      </summary>
      <div className="mt-2 space-y-2">
        <div className="flex flex-wrap gap-x-4 gap-y-1" role="radiogroup" aria-label={t('pins.for', { name: person.name })}>
          {(['role', 'any', 'only'] as const).map((m) => (
            <label key={m} className="flex items-center gap-1.5 text-sm">
              <input
                type="radio"
                name={`pins-${person.id}`}
                checked={shown === m}
                disabled={save.isPending}
                onChange={() => {
                  setPicking(m === 'only' && mode !== 'only')
                  if (m === 'role') save.mutate(null)
                  if (m === 'any') save.mutate([])
                }}
              />
              {t(`pins.mode.${m}`)}
            </label>
          ))}
        </div>
        {shown === 'only' ? (
          <>
            <Checklist
              value={person.profiles ?? []}
              label={t('pins.for', { name: person.name })}
              disabled={save.isPending}
              onChange={(next) => {
                // None ticked would be any profile; "Any profile" says that.
                if (next.length === 0) return
                setPicking(false)
                save.mutate(next)
              }}
            />
            <p className="text-ink-faint">{t('pins.pickOne')}</p>
          </>
        ) : null}
        {error ? <ShownError {...error} className="text-danger" /> : null}
      </div>
    </details>
  )
}
