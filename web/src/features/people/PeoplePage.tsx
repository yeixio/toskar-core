import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { RealmKicker } from '@/components/ui/Realm'
import { formatDate } from '@/i18n/format'
import { api, ApiError } from '@/lib/api'
import type { Person, PersonLink, Role } from '@/types/api'
import { PersonCapabilities, RoleCapabilities } from './Capabilities'
import { PersonPins, RolePins } from './ProfilePins'
import { useShareBase } from './shareBase'
import { ShownError, shownError, type Shown } from '@/components/ui/ShownError'

const ROLES: Role[] = ['admin', 'member', 'visitor']

/** Whether actor may change people with role (the server checks too). */
function mayManage(actor: Role | undefined, role: Role): boolean {
  if (actor === 'owner') return role !== 'owner'
  if (actor === 'admin') return role === 'member' || role === 'visitor'
  return false
}

/** A link to give someone, with a copy button, and when it stops working. */
function LinkPanel({ name, link, onDone }: { name: string; link: PersonLink; onDone: () => void }) {
  const { t } = useTranslation('people')
  const { base, reachable } = useShareBase()
  const [copied, setCopied] = useState(false)
  const url = `${base}${link.path}`
  return (
    <div className="space-y-2 rounded-xl border border-primary/30 bg-primary/5 p-3 text-sm" role="status">
      <p className="text-ink">
        {link.kind === 'invite' ? t('link.invite', { name }) : t('link.reset', { name })}
      </p>
      <div className="flex min-w-0 items-stretch gap-2">
        <code className="field min-w-0 flex-1 truncate py-2 font-mono text-xs text-ink">{url}</code>
        <button
          type="button"
          className="btn-secondary btn-sm shrink-0"
          onClick={() => {
            void navigator.clipboard?.writeText(url).then(() => setCopied(true))
          }}
        >
          {copied ? t('link.copied') : t('link.copy')}
        </button>
      </div>
      <p className="text-xs text-ink-muted">
        {t('link.expires', { date: formatDate(link.expires_at, { dateStyle: 'medium', timeStyle: 'short' }) })}
      </p>
      {!reachable ? (
        <p className="text-xs text-warning">
          {t('link.lanOff')}{' '}
          <Link to="/api-access" className="text-primary hover:underline">
            {t('link.lanOffAction')}
          </Link>
        </p>
      ) : null}
      <button type="button" className="text-xs text-ink-faint hover:text-ink" onClick={onDone}>
        {t('link.done')}
      </button>
    </div>
  )
}

function PersonRow({ person, me, onLink }: { person: Person; me?: Person; onLink: (person: Person, link: PersonLink) => void }) {
  const { t } = useTranslation('people')
  const queryClient = useQueryClient()
  const [error, setError] = useState<Shown | null>(null)
  const self = person.id === me?.id
  const canManage = !self && mayManage(me?.role, person.role)
  const change = useMutation({
    mutationFn: (c: { role?: Role; disabled?: boolean }) => api.changePerson(person.id, c),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ['people'] }),
    onError: (err) => setError(shownError(err)),
  })
  const link = useMutation({
    mutationFn: () => api.personLink(person.id),
    onSuccess: (l) => l && onLink(person, l),
    onError: (err) => setError(shownError(err)),
  })
  const disabled = !!person.disabled_at
  return (
    <li className={['flex flex-wrap items-center gap-x-4 gap-y-2 py-3', disabled ? 'opacity-70' : ''].join(' ')}>
      <div className="min-w-0 flex-1">
        <p className="font-medium text-ink">
          {person.name}
          {self ? <span className="ms-2 text-xs text-ink-faint">{t('you')}</span> : null}
        </p>
        <p className="text-xs text-ink-muted">
          {person.external
            ? t('byProxy', { name: person.external })
            : person.sign_in && person.username
              ? `@${person.username}`
              : person.role === 'owner'
                ? t('ownerNoSignIn')
                : t('notSignedIn')}
          {disabled ? <span className="status-chip ms-2 bg-danger/15 text-danger">{t('disabled')}</span> : null}
        </p>
        {error ? <ShownError {...error} className="mt-1 text-xs text-danger" /> : null}
        {canManage && !disabled && (person.role === 'member' || person.role === 'visitor') ? (
          <div className="mt-1">
            <PersonPins person={person} />
          </div>
        ) : null}
        {canManage && !disabled ? (
          <div className="mt-1">
            <PersonCapabilities person={person} />
          </div>
        ) : null}
      </div>
      {/* A proxied person's role follows their groups there. */}
      {canManage && !disabled && !person.external ? (
        <select
          className="field w-auto py-1 text-sm"
          aria-label={t('roleFor', { name: person.name })}
          value={person.role}
          onChange={(e) => change.mutate({ role: e.target.value as Role })}
        >
          {ROLES.filter((r) => mayManage(me?.role, r) || r === person.role).map((r) => (
            <option key={r} value={r}>
              {t(`roles.${r}`)}
            </option>
          ))}
        </select>
      ) : (
        <span className="text-sm text-ink-muted">{t(`roles.${person.role}`)}</span>
      )}
      <div className="flex gap-2">
        {(canManage || self) && !disabled && !person.external ? (
          <button type="button" className="btn-secondary btn-sm" disabled={link.isPending} onClick={() => link.mutate()}>
            {person.sign_in ? t('newPasswordLink') : t('signInLink')}
          </button>
        ) : null}
        {canManage ? (
          <button
            type="button"
            className="btn-secondary btn-sm"
            disabled={change.isPending}
            onClick={() => change.mutate({ disabled: !disabled })}
          >
            {disabled ? t('enable') : t('disable')}
          </button>
        ) : null}
      </div>
    </li>
  )
}

/**
 * Administer → People (#206): who uses this Toskar and their roles. Admins
 * and the Owner add people, who get a one-time link to choose a username
 * and password, change roles, and disable people. Each person's chats,
 * memories, files, and automations are their own.
 */
export function PeoplePage() {
  const { t } = useTranslation('people')
  const queryClient = useQueryClient()
  const me = useQuery({ queryKey: ['me'], queryFn: () => api.getMe() })
  const people = useQuery({ queryKey: ['people'], queryFn: () => api.listPeople(), retry: false })
  const [name, setName] = useState('')
  const [role, setRole] = useState<Role>('member')
  const [error, setError] = useState<Shown | null>(null)
  const [shown, setShown] = useState<{ name: string; link: PersonLink } | null>(null)
  const add = useMutation({
    mutationFn: () => api.addPerson(name.trim(), role),
    onSuccess: (added) => {
      if (!added) return
      const { person, link } = added
      setName('')
      setError(null)
      setShown({ name: person.name, link })
      void queryClient.invalidateQueries({ queryKey: ['people'] })
    },
    onError: (err) => setError(shownError(err)),
  })
  const actor = me.data?.person
  const refused = people.error instanceof ApiError && people.error.status === 403

  return (
    <div className="mx-auto w-full max-w-2xl min-w-0 space-y-6">
      <header className="page-header">
        <RealmKicker />
        <h1 className="page-title">{t('nav.people', { ns: 'common' })}</h1>
        <p className="page-subtitle">{t('subtitle')}</p>
      </header>

      {refused ? (
        <section className="card text-sm text-ink-muted">{t('adminsOnly')}</section>
      ) : (
        <>
          <section className="card space-y-3" aria-labelledby="add-person">
            <h2 id="add-person" className="section-title">
              {t('add.title')}
            </h2>
            <p className="text-sm text-ink-muted">{t('add.body')}</p>
            <form
              className="flex flex-wrap items-end gap-2"
              onSubmit={(e) => {
                e.preventDefault()
                if (name.trim()) add.mutate()
              }}
            >
              <label className="min-w-[12rem] flex-1 space-y-1 text-sm">
                <span className="text-ink-muted">{t('add.name')}</span>
                <input className="field w-full" value={name} onChange={(e) => setName(e.target.value)} maxLength={80} />
              </label>
              <label className="space-y-1 text-sm">
                <span className="text-ink-muted">{t('add.role')}</span>
                <select className="field w-auto" value={role} onChange={(e) => setRole(e.target.value as Role)}>
                  {ROLES.filter((r) => mayManage(actor?.role, r)).map((r) => (
                    <option key={r} value={r}>
                      {t(`roles.${r}`)}
                    </option>
                  ))}
                </select>
              </label>
              <button type="submit" className="btn-primary" disabled={!name.trim() || add.isPending}>
                {t('add.submit')}
              </button>
            </form>
            {error ? <ShownError {...error} className="text-sm text-danger" /> : null}
            <p className="text-xs text-ink-faint">{t(`add.roleHint.${role}`)}</p>
          </section>

          {shown ? <LinkPanel name={shown.name} link={shown.link} onDone={() => setShown(null)} /> : null}

          <RoleCapabilities actor={actor?.role} />

          <RolePins />

          <section className="card" aria-labelledby="everyone">
            <h2 id="everyone" className="section-title">
              {t('everyone')}
            </h2>
            <ul className="divide-y divide-line">
              {(people.data ?? []).map((p) => (
                <PersonRow key={p.id} person={p} me={actor} onLink={(person, link) => setShown({ name: person.name, link })} />
              ))}
            </ul>
            <p className="mt-3 text-xs text-ink-faint">{t('private')}</p>
          </section>
        </>
      )}
    </div>
  )
}
