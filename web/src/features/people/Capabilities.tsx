import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '@/lib/api'
import type { Capability, Person, Role } from '@/types/api'
import { ShownError, shownError, type Shown } from '@/components/ui/ShownError'

/**
 * What people may use (#589): each role's capabilities, which Admins
 * change for Members and Visitors and the Owner for Admins too, and one
 * person's Allow or Block in place of their role's. The Owner always has
 * everything.
 */

const EDITABLE: Role[] = ['admin', 'member', 'visitor']

function useRoleCapabilities() {
  return useQuery({ queryKey: ['role-capabilities'], queryFn: () => api.getRoleCapabilities(), retry: false })
}

/** The section with each role's capabilities, as a grid of switches. */
export function RoleCapabilities({ actor }: { actor?: Role }) {
  const { t } = useTranslation('people')
  const queryClient = useQueryClient()
  const roles = useRoleCapabilities()
  const [error, setError] = useState<Shown | null>(null)
  const save = useMutation({
    mutationFn: ({ role, capability, on }: { role: Role; capability: Capability; on: boolean }) =>
      api.setRoleCapabilities(role, { ...roles.data?.roles[role], [capability]: on }),
    onSuccess: () => {
      setError(null)
      void queryClient.invalidateQueries({ queryKey: ['role-capabilities'] })
      void queryClient.invalidateQueries({ queryKey: ['person-capabilities'] })
    },
    onError: (err) => setError(shownError(err)),
  })
  if (!roles.data?.capabilities) return null
  const { capabilities, roles: current, defaults } = roles.data
  return (
    <section className="card space-y-3" aria-labelledby="role-capabilities">
      <h2 id="role-capabilities" className="section-title">
        {t('capabilities.title')}
      </h2>
      <p className="text-sm text-ink-muted">{t('capabilities.body')}</p>
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="text-start text-xs text-ink-muted">
              <th scope="col" className="py-1 pe-3 text-start font-medium">
                {t('capabilities.capability')}
              </th>
              {EDITABLE.map((role) => (
                <th key={role} scope="col" className="px-2 py-1 text-center font-medium">
                  {t(`roles.${role}`)}
                </th>
              ))}
            </tr>
          </thead>
          <tbody className="divide-y divide-line">
            {capabilities.map((c) => (
              <tr key={c}>
                <th scope="row" className="py-2 pe-3 text-start font-normal">
                  <span className="text-ink">{t(`capabilities.names.${c}`)}</span>
                  <span className="block text-xs text-ink-faint">{t(`capabilities.hints.${c}`)}</span>
                </th>
                {EDITABLE.map((role) => {
                  const on = current[role]?.[c] !== false
                  // Only the Owner changes what Admins may use.
                  const locked = role === 'admin' && actor !== 'owner'
                  const changed = on !== (defaults[role]?.[c] !== false)
                  return (
                    <td key={role} className="relative px-2 py-2 text-center">
                      <input
                        type="checkbox"
                        checked={on}
                        disabled={locked || save.isPending}
                        aria-label={t('capabilities.forRole', { capability: t(`capabilities.names.${c}`), role: t(`roles.${role}`) })}
                        onChange={(e) => save.mutate({ role, capability: c, on: e.target.checked })}
                      />
                      {changed ? (
                        <span className="absolute ms-1 text-xs text-primary" title={t('capabilities.changed')}>
                          •
                        </span>
                      ) : null}
                    </td>
                  )
                })}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <p className="text-xs text-ink-faint">{t('capabilities.owner')}</p>
      {error ? <ShownError {...error} className="text-sm text-danger" /> : null}
    </section>
  )
}

type Choice = 'role' | 'allow' | 'block'

/** One person's capabilities: each as their role's, Allow, or Block. */
export function PersonCapabilities({ person }: { person: Person }) {
  const { t } = useTranslation('people')
  const queryClient = useQueryClient()
  const roles = useRoleCapabilities()
  const [open, setOpen] = useState(false)
  const caps = useQuery({
    queryKey: ['person-capabilities', person.id],
    queryFn: () => api.getPersonCapabilities(person.id),
    enabled: open,
    retry: false,
  })
  const [error, setError] = useState<Shown | null>(null)
  const save = useMutation({
    mutationFn: (set: Partial<Record<Capability, boolean | null>>) => api.setPersonCapabilities(person.id, set),
    onSuccess: (next) => {
      setError(null)
      if (next) queryClient.setQueryData(['person-capabilities', person.id], next)
    },
    onError: (err) => setError(shownError(err)),
  })
  const set = caps.data?.set ?? {}
  const changed = Object.keys(set).length
  return (
    <details className="w-full text-xs" onToggle={(e) => setOpen((e.target as HTMLDetailsElement).open)}>
      <summary className="cursor-pointer text-ink-muted">
        {t('capabilities.personSummary')}
        {caps.data ? (
          <>
            : <span className="text-ink">{changed ? t('capabilities.personChanged', { count: changed }) : t('capabilities.asRole')}</span>
          </>
        ) : null}
      </summary>
      {caps.data?.set && roles.data?.capabilities ? (
        <div className="mt-2 grid gap-x-4 gap-y-1.5 sm:grid-cols-2">
          {roles.data.capabilities.map((c) => {
            const choice: Choice = set[c] === undefined ? 'role' : set[c] ? 'allow' : 'block'
            const roleOn = roles.data?.roles[person.role]?.[c] !== false
            return (
              <label key={c} className="flex items-center justify-between gap-2 text-sm">
                <span className="truncate text-ink">{t(`capabilities.names.${c}`)}</span>
                <select
                  className="field w-auto py-0.5 text-xs"
                  value={choice}
                  disabled={save.isPending}
                  aria-label={t('capabilities.forPerson', { capability: t(`capabilities.names.${c}`), name: person.name })}
                  onChange={(e) => {
                    const next: Partial<Record<Capability, boolean | null>> = { ...set }
                    const v = e.target.value as Choice
                    next[c] = v === 'role' ? null : v === 'allow'
                    save.mutate(next)
                  }}
                >
                  <option value="role">{roleOn ? t('capabilities.roleOn') : t('capabilities.roleOff')}</option>
                  <option value="allow">{t('capabilities.allow')}</option>
                  <option value="block">{t('capabilities.block')}</option>
                </select>
              </label>
            )
          })}
        </div>
      ) : null}
      {error ? <ShownError {...error} className="mt-1 text-danger" /> : null}
    </details>
  )
}
