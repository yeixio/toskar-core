import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { RealmKicker } from '@/components/ui/Realm'
import { Toggle } from '@/components/ui/Toggle'
import { useShareBase } from '@/features/people/shareBase'
import { PortalPreview } from '@/features/portal/PortalPreview'
import { availableLanguages, languages } from '@/i18n'
import { formatDate } from '@/i18n/format'
import { api } from '@/lib/api'
import type { Person, Portal, PortalBranding, PortalInput, PortalVisitorLink } from '@/types/api'
import { slugFrom } from './slug'
import { ShownError, shownError } from '@/components/ui/ShownError'
import { LoadError } from '@/components/ui/LoadError'
import { Skeleton } from '@/components/ui/Skeleton'

function usePrefersDark(): boolean {
  return typeof window !== 'undefined' && window.matchMedia ? window.matchMedia('(prefers-color-scheme: dark)').matches : true
}

/** The address visitors open, with a copy button. */
function PortalLink({ slug }: { slug: string }) {
  const { t } = useTranslation('portals')
  const { base, reachable } = useShareBase()
  const [copied, setCopied] = useState(false)
  const url = `${base}/p/${slug}`
  return (
    <div className="space-y-1">
      <div className="flex min-w-0 items-stretch gap-2">
        <code className="field min-w-0 flex-1 truncate py-1.5 font-mono text-xs">{url}</code>
        <button
          type="button"
          className="btn-secondary btn-sm shrink-0"
          onClick={() => void navigator.clipboard?.writeText(url).then(() => setCopied(true))}
        >
          {copied ? t('link.copied') : t('link.copy')}
        </button>
      </div>
      {!reachable ? (
        <p className="text-xs text-warning">
          {t('link.lanOff')}{' '}
          {/* Underlined in the warning's own colour: told apart by more than colour (WCAG 1.4.1). */}
          <Link to="/api-access" className="font-medium underline underline-offset-2 hover:no-underline">
            {t('link.lanOffAction')}
          </Link>
        </p>
      ) : null}
    </div>
  )
}

function AddPortal({ onAdded }: { onAdded: (p: Portal) => void }) {
  const { t } = useTranslation('portals')
  const queryClient = useQueryClient()
  const [name, setName] = useState('')
  const [slug, setSlug] = useState('')
  const [slugTouched, setSlugTouched] = useState(false)
  const [access, setAccess] = useState<Portal['access']>('passcode')
  const [passcode, setPasscode] = useState('')
  const add = useMutation({
    mutationFn: () => api.createPortal({ name: name.trim(), slug, access, ...(access === 'passcode' ? { passcode } : {}) }),
    onSuccess: (p) => {
      setName('')
      setSlug('')
      setSlugTouched(false)
      setPasscode('')
      void queryClient.invalidateQueries({ queryKey: ['portals'] })
      if (p) onAdded(p)
    },
  })
  return (
    <section className="card space-y-3" aria-labelledby="add-portal">
      <h2 id="add-portal" className="section-title">
        {t('add.title')}
      </h2>
      <p className="text-sm text-ink-muted">{t('add.body')}</p>
      <form
        className="grid gap-3 sm:grid-cols-2"
        onSubmit={(e) => {
          e.preventDefault()
          add.mutate()
        }}
      >
        <label className="space-y-1 text-sm">
          <span className="text-ink-muted">{t('field.name')}</span>
          <input
            className="field w-full"
            value={name}
            maxLength={80}
            onChange={(e) => {
              setName(e.target.value)
              if (!slugTouched) setSlug(slugFrom(e.target.value))
            }}
          />
        </label>
        <label className="space-y-1 text-sm">
          <span className="text-ink-muted">{t('field.slug')}</span>
          <input
            className="field w-full font-mono"
            value={slug}
            maxLength={40}
            onChange={(e) => {
              setSlugTouched(true)
              setSlug(e.target.value.toLowerCase())
            }}
          />
        </label>
        <label className="space-y-1 text-sm">
          <span className="text-ink-muted">{t('field.access')}</span>
          <select className="field w-full" value={access} onChange={(e) => setAccess(e.target.value as Portal['access'])}>
            <option value="passcode">{t('access.passcode')}</option>
            <option value="open">{t('access.open')}</option>
            <option value="members">{t('access.members')}</option>
            <option value="invited">{t('access.invited')}</option>
          </select>
        </label>
        {access === 'passcode' ? (
          <label className="space-y-1 text-sm">
            <span className="text-ink-muted">{t('field.passcode')}</span>
            <input className="field w-full" value={passcode} autoComplete="off" onChange={(e) => setPasscode(e.target.value)} />
          </label>
        ) : (
          <p className="self-end text-xs text-ink-faint">{t(`access.${access}Hint`)}</p>
        )}
        {add.error ? <ShownError {...shownError(add.error)} role="alert" className="text-sm text-danger sm:col-span-2" /> : null}
        <div className="sm:col-span-2">
          <button
            type="submit"
            className="btn-primary"
            disabled={add.isPending || !name.trim() || slug.length < 2 || (access === 'passcode' && passcode.trim().length < 6)}
          >
            {t('add.submit')}
          </button>
        </div>
      </form>
    </section>
  )
}

/** Branding as the editor holds it: every field a string. */
type BrandingDraft = Required<Omit<PortalBranding, 'prompts' | 'theme'>> & { theme: string; prompts: string }

function toDraft(b: PortalBranding): BrandingDraft {
  return {
    title: b.title ?? '',
    logo_url: b.logo_url ?? '',
    accent: b.accent ?? '',
    background: b.background ?? '',
    theme: b.theme ?? 'system',
    welcome: b.welcome ?? '',
    prompts: (b.prompts ?? []).join('\n'),
    footer: b.footer ?? '',
  }
}

function fromDraft(d: BrandingDraft): PortalBranding {
  const out: PortalBranding = { theme: d.theme as PortalBranding['theme'] }
  for (const key of ['title', 'logo_url', 'accent', 'background', 'welcome', 'footer'] as const) {
    const v = d[key].trim()
    if (v) out[key] = v
  }
  const lines = d.prompts
    .split('\n')
    .map((l) => l.trim())
    .filter(Boolean)
    .slice(0, 6)
  if (lines.length) out.prompts = lines
  return out
}

/** A portal's invited visitors, each with a one-time link (#205). */
function InvitedVisitors({ portal }: { portal: Portal }) {
  const { t } = useTranslation('portals')
  const queryClient = useQueryClient()
  const { base } = useShareBase()
  const [name, setName] = useState('')
  const [shown, setShown] = useState<{ name: string; url: string; expires: string } | null>(null)
  const [copied, setCopied] = useState(false)
  const visitors = useQuery({ queryKey: ['portal-visitors', portal.id], queryFn: () => api.listPortalVisitors(portal.id), retry: false })
  const refresh = () => void queryClient.invalidateQueries({ queryKey: ['portal-visitors', portal.id] })
  const show = (who: string, link: PortalVisitorLink | null) => {
    if (!link) return
    setCopied(false)
    setShown({ name: who, url: `${base}${link.path}`, expires: link.expires_at })
  }
  const invite = useMutation({
    mutationFn: () => api.invitePortalVisitor(portal.id, name.trim()),
    onSuccess: (made) => {
      setName('')
      refresh()
      if (made) show(made.person.name, made.link)
    },
  })
  const relink = useMutation({
    mutationFn: (v: Person) => api.portalVisitorLink(portal.id, v.id).then((link) => ({ v, link })),
    onSuccess: ({ v, link }) => show(v.name, link),
  })
  const remove = useMutation({ mutationFn: (v: Person) => api.removePortalVisitor(portal.id, v.id), onSuccess: refresh })
  const error = invite.error ?? relink.error ?? remove.error
  return (
    <div className="space-y-3 rounded-lg border border-line/60 p-3">
      <p className="text-sm font-medium text-ink">{t('visitors.title')}</p>
      <form
        className="flex flex-wrap items-end gap-2"
        onSubmit={(e) => {
          e.preventDefault()
          if (name.trim()) invite.mutate()
        }}
      >
        <label className="min-w-[10rem] flex-1 space-y-1 text-sm">
          <span className="text-ink-muted">{t('visitors.name')}</span>
          <input className="field w-full" value={name} maxLength={80} onChange={(e) => setName(e.target.value)} />
        </label>
        <button type="submit" className="btn-secondary" disabled={!name.trim() || invite.isPending}>
          {t('visitors.invite')}
        </button>
      </form>
      {shown ? (
        <div className="space-y-1.5 rounded-lg bg-primary/5 p-2 text-sm" role="status">
          <p className="text-ink">{t('visitors.send', { name: shown.name })}</p>
          <div className="flex min-w-0 items-stretch gap-2">
            <code className="field min-w-0 flex-1 truncate py-1.5 font-mono text-xs">{shown.url}</code>
            <button
              type="button"
              className="btn-secondary btn-sm shrink-0"
              onClick={() => void navigator.clipboard?.writeText(shown.url).then(() => setCopied(true))}
            >
              {copied ? t('link.copied') : t('link.copy')}
            </button>
          </div>
          <p className="text-xs text-ink-muted">
            {t('visitors.expires', { date: formatDate(shown.expires, { dateStyle: 'medium', timeStyle: 'short' }) })}
          </p>
        </div>
      ) : null}
      {error ? <ShownError {...shownError(error)} className="text-xs text-danger" /> : null}
      {visitors.isError ? (
        <LoadError compact error={visitors.error} onRetry={() => void visitors.refetch()} retrying={visitors.isFetching} />
      ) : null}
      {visitors.isLoading ? <p className="text-xs text-ink-faint">…</p> : null}
      {visitors.isSuccess && (visitors.data ?? []).length === 0 ? <p className="text-xs text-ink-faint">{t('visitors.none')}</p> : null}
      <ul className="divide-y divide-line">
        {(visitors.data ?? []).map((v) => (
          <li key={v.id} className="flex flex-wrap items-center gap-2 py-2">
            <span className="min-w-0 flex-1 truncate text-sm text-ink">
              {v.name}
              {v.disabled_at ? <span className="status-chip ms-2 bg-ink/10 text-ink-muted">{t('visitors.removed')}</span> : null}
            </span>
            {!v.disabled_at ? (
              <>
                <button type="button" className="btn-secondary btn-sm" disabled={relink.isPending} onClick={() => relink.mutate(v)}>
                  {t('visitors.newLink')}
                </button>
                <button
                  type="button"
                  className="btn-secondary btn-sm"
                  disabled={remove.isPending}
                  onClick={() => {
                    if (window.confirm(t('visitors.removeConfirm', { name: v.name }))) remove.mutate(v)
                  }}
                >
                  {t('remove.button')}
                </button>
              </>
            ) : null}
          </li>
        ))}
      </ul>
    </div>
  )
}

/** Snippets that put a portal on another website (#205). */
function EmbedSnippets({ slug, name }: { slug: string; name: string }) {
  const { t } = useTranslation('portals')
  const { base } = useShareBase()
  const [copied, setCopied] = useState('')
  const quoted = name.replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[c] ?? c)
  const snippets = [
    { id: 'button', label: t('embed.button'), code: `<script src="${base}/embed.js" data-portal="${slug}" data-label="${quoted}"></script>` },
    { id: 'frame', label: t('embed.frame'), code: `<iframe src="${base}/p/${slug}" title="${quoted}" style="width:380px;height:600px;border:0"></iframe>` },
  ]
  return (
    <div className="space-y-2">
      {snippets.map((s) => (
        <div key={s.id} className="space-y-1">
          <p className="text-xs text-ink-muted">{s.label}</p>
          <div className="flex min-w-0 items-stretch gap-2">
            <code className="field min-w-0 flex-1 truncate py-1.5 font-mono text-xs">{s.code}</code>
            <button
              type="button"
              className="btn-secondary btn-sm shrink-0"
              onClick={() => void navigator.clipboard?.writeText(s.code).then(() => setCopied(s.id))}
            >
              {copied === s.id ? t('link.copied') : t('link.copy')}
            </button>
          </div>
        </div>
      ))}
    </div>
  )
}

function PortalEditor({ portal, onClose }: { portal: Portal; onClose: () => void }) {
  const { t } = useTranslation('portals')
  const queryClient = useQueryClient()
  const prefersDark = usePrefersDark()
  const profiles = useQuery({ queryKey: ['profiles'], queryFn: () => api.getProfiles(), retry: false })
  const [name, setName] = useState(portal.name)
  const [slug, setSlug] = useState(portal.slug)
  const [profileId, setProfileId] = useState(portal.profile_id)
  const [tools, setTools] = useState(portal.tools)
  const [memory, setMemory] = useState(portal.memory)
  const [language, setLanguage] = useState(portal.language)
  const [access, setAccess] = useState(portal.access)
  const [passcode, setPasscode] = useState('')
  const [origins, setOrigins] = useState((portal.embed_origins ?? []).join('\n'))
  const [retention, setRetention] = useState(String(portal.retention_days ?? 30))
  const [hourly, setHourly] = useState(String(portal.hourly_limit))
  const [longest, setLongest] = useState(String(portal.max_message))
  const [most, setMost] = useState(String(portal.concurrency))
  const [draft, setDraft] = useState(() => toDraft(portal.branding ?? {}))
  const [saved, setSaved] = useState(false)
  const branding = useMemo(() => fromDraft(draft), [draft])
  const set = (key: keyof BrandingDraft) => (value: string) => {
    setSaved(false)
    setDraft((d) => ({ ...d, [key]: value }))
  }

  const save = useMutation({
    mutationFn: () => {
      const input: PortalInput = {
        name: name.trim(), slug, profile_id: profileId, tools, memory, language, access, branding,
        hourly_limit: Number(hourly), max_message: Number(longest), concurrency: Number(most),
        embed_origins: origins.split('\n').map((o) => o.trim()).filter(Boolean),
        retention_days: Number(retention),
      }
      if (passcode.trim()) input.passcode = passcode.trim()
      return api.updatePortal(portal.id, input)
    },
    onSuccess: () => {
      setPasscode('')
      setSaved(true)
      void queryClient.invalidateQueries({ queryKey: ['portals'] })
    },
  })
  useEffect(() => setSaved(false), [name, slug, profileId, tools, memory, language, access, passcode, hourly, longest, most, origins, retention])

  const choices = languages.filter((l) => availableLanguages.includes(l.code))
  const field = 'space-y-1 text-sm'
  const label = 'text-ink-muted'

  return (
    <section className="card space-y-4" aria-labelledby="edit-portal">
      <div className="flex items-start justify-between gap-3">
        <h2 id="edit-portal" className="section-title">
          {t('edit.title', { name: portal.name })}
        </h2>
        <button type="button" className="btn-secondary btn-sm" onClick={onClose}>
          {t('edit.close')}
        </button>
      </div>
      <form
        className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_18rem]"
        onSubmit={(e) => {
          e.preventDefault()
          save.mutate()
        }}
      >
        <div className="min-w-0 space-y-5">
          <fieldset className="grid gap-3 sm:grid-cols-2">
            <legend className="label-caps mb-2">{t('edit.answers')}</legend>
            <label className={field}>
              <span className={label}>{t('field.name')}</span>
              <input className="field w-full" value={name} maxLength={80} onChange={(e) => setName(e.target.value)} />
            </label>
            <label className={field}>
              <span className={label}>{t('field.slug')}</span>
              <input className="field w-full font-mono" value={slug} maxLength={40} onChange={(e) => setSlug(e.target.value.toLowerCase())} />
            </label>
            <label className={field}>
              <span className={label}>{t('field.profile')}</span>
              <select className="field w-full" value={profileId} onChange={(e) => setProfileId(e.target.value)}>
                <option value="">{t('field.defaultProfile')}</option>
                {/* Kept when the profiles couldn't be read, so the choice still shows (#217). */}
                {profileId && !(profiles.data ?? []).some((p) => p.id === profileId) ? <option value={profileId}>{profileId}</option> : null}
                {(profiles.data ?? []).map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name}
                  </option>
                ))}
              </select>
            </label>
            <label className={field}>
              <span className={label}>{t('field.tools')}</span>
              <select className="field w-full" value={tools} onChange={(e) => setTools(e.target.value as Portal['tools'])}>
                <option value="none">{t('tools.none')}</option>
                <option value="read_only">{t('tools.read_only')}</option>
                <option value="profile">{t('tools.profile')}</option>
              </select>
            </label>
            <label className={field}>
              <span className={label}>{t('field.language')}</span>
              <select className="field w-full" value={language} onChange={(e) => setLanguage(e.target.value)}>
                <option value="">{t('field.followVisitor')}</option>
                {choices.map((l) => (
                  <option key={l.code} value={l.code} lang={l.code}>
                    {l.name}
                  </option>
                ))}
              </select>
            </label>
            <div className="flex items-center justify-between gap-3 self-end rounded-lg border border-line/60 px-3 py-2">
              <span className="text-sm text-ink">{t('field.memory')}</span>
              <Toggle label={t('field.memory')} checked={memory} onChange={() => setMemory(!memory)} />
            </div>
          </fieldset>

          <fieldset className="grid gap-3 sm:grid-cols-2">
            <legend className="label-caps mb-2">{t('edit.access')}</legend>
            <label className={field}>
              <span className={label}>{t('field.access')}</span>
              <select className="field w-full" value={access} onChange={(e) => setAccess(e.target.value as Portal['access'])}>
                <option value="passcode">{t('access.passcode')}</option>
                <option value="open">{t('access.open')}</option>
                <option value="members">{t('access.members')}</option>
                <option value="invited">{t('access.invited')}</option>
              </select>
            </label>
            {access === 'passcode' ? (
              <label className={field}>
                <span className={label}>{portal.has_passcode ? t('field.newPasscode') : t('field.passcode')}</span>
                <input
                  className="field w-full"
                  value={passcode}
                  autoComplete="off"
                  placeholder={portal.has_passcode ? t('field.keepPasscode') : ''}
                  onChange={(e) => setPasscode(e.target.value)}
                />
              </label>
            ) : (
              <p className="self-end text-xs text-ink-faint">{t(`access.${access}Hint`)}</p>
            )}
            {access === 'invited' ? (
              <div className="sm:col-span-2">
                {portal.access === 'invited' ? <InvitedVisitors portal={portal} /> : <p className="text-xs text-ink-faint">{t('visitors.saveFirst')}</p>}
              </div>
            ) : null}
          </fieldset>

          <fieldset className="grid gap-3 sm:grid-cols-2">
            <legend className="label-caps mb-2">{t('edit.limits')}</legend>
            <label className={field}>
              <span className={label}>{t('limit.hourly')}</span>
              <input className="field w-full" type="number" min={0} max={1000} value={hourly} onChange={(e) => setHourly(e.target.value)} />
            </label>
            <label className={field}>
              <span className={label}>{t('limit.longest')}</span>
              <input className="field w-full" type="number" min={100} max={20000} step={100} value={longest} onChange={(e) => setLongest(e.target.value)} />
            </label>
            <label className={field}>
              <span className={label}>{t('limit.most')}</span>
              <input className="field w-full" type="number" min={1} max={20} value={most} onChange={(e) => setMost(e.target.value)} />
            </label>
            <label className={field}>
              <span className={label}>{t('limit.keep')}</span>
              <select className="field w-full" value={retention} onChange={(e) => setRetention(e.target.value)}>
                {['7', '30', '90', '365', '0'].map((d) => (
                  <option key={d} value={d}>
                    {d === '0' ? t('limit.keepForever') : t('limit.keepDays', { count: Number(d) })}
                  </option>
                ))}
              </select>
            </label>
            <p className="text-xs text-ink-faint sm:col-span-2">{t('limit.hint')}</p>
          </fieldset>

          <fieldset className="grid gap-3 sm:grid-cols-2">
            <legend className="label-caps mb-2">{t('edit.looks')}</legend>
            <label className={field}>
              <span className={label}>{t('brand.title')}</span>
              <input className="field w-full" value={draft.title} maxLength={80} placeholder={name} onChange={(e) => set('title')(e.target.value)} />
            </label>
            <label className={field}>
              <span className={label}>{t('brand.logo')}</span>
              <input className="field w-full" value={draft.logo_url} placeholder="https://" onChange={(e) => set('logo_url')(e.target.value)} />
            </label>
            {(['accent', 'background'] as const).map((key) => (
              <label key={key} className={field}>
                <span className={label}>{t(`brand.${key}`)}</span>
                <span className="flex gap-2">
                  <input
                    type="color"
                    className="h-9 w-10 shrink-0 cursor-pointer rounded border border-line bg-transparent"
                    aria-label={t(`brand.${key}`)}
                    value={/^#[0-9a-f]{6}$/i.test(draft[key]) ? draft[key] : key === 'accent' ? '#4fd1c5' : '#0b0f14'}
                    onChange={(e) => set(key)(e.target.value)}
                  />
                  <input
                    className="field min-w-0 flex-1 font-mono"
                    value={draft[key]}
                    placeholder={t('brand.themeColour')}
                    onChange={(e) => set(key)(e.target.value)}
                  />
                </span>
              </label>
            ))}
            <label className={field}>
              <span className={label}>{t('brand.theme')}</span>
              <select className="field w-full" value={draft.theme} onChange={(e) => set('theme')(e.target.value)}>
                <option value="system">{t('theme.system')}</option>
                <option value="light">{t('theme.light')}</option>
                <option value="dark">{t('theme.dark')}</option>
              </select>
            </label>
            <label className={`${field} sm:col-span-2`}>
              <span className={label}>{t('brand.welcome')}</span>
              <textarea className="field w-full" rows={2} value={draft.welcome} onChange={(e) => set('welcome')(e.target.value)} />
            </label>
            <label className={field}>
              <span className={label}>{t('brand.prompts')}</span>
              <textarea className="field w-full" rows={3} value={draft.prompts} onChange={(e) => set('prompts')(e.target.value)} />
            </label>
            <label className={field}>
              <span className={label}>{t('brand.footer')}</span>
              <textarea className="field w-full" rows={3} value={draft.footer} onChange={(e) => set('footer')(e.target.value)} />
            </label>
          </fieldset>

          <fieldset className="space-y-3">
            <legend className="label-caps mb-2">{t('edit.embed')}</legend>
            <label className={field}>
              <span className={label}>{t('embed.origins')}</span>
              <textarea
                className="field w-full font-mono"
                rows={2}
                placeholder="https://shop.example.com"
                value={origins}
                onChange={(e) => setOrigins(e.target.value)}
              />
            </label>
            <p className="text-xs text-ink-faint">{t('embed.hint')}</p>
            {portal.embed_origins.length > 0 ? <EmbedSnippets slug={portal.slug} name={portal.name} /> : null}
          </fieldset>

          {save.error ? <ShownError {...shownError(save.error)} role="alert" /> : null}
          <div className="flex items-center gap-3">
            <button type="submit" className="btn-primary" disabled={save.isPending || !name.trim() || slug.length < 2}>
              {t('edit.save')}
            </button>
            {saved ? (
              <span className="text-sm text-success" role="status">
                {t('edit.saved')}
              </span>
            ) : null}
          </div>
        </div>
        <div className="space-y-2 lg:sticky lg:top-4 lg:self-start">
          <p className="label-caps">{t('edit.preview')}</p>
          <PortalPreview name={name} branding={branding} prefersDark={prefersDark} />
        </div>
      </form>
    </section>
  )
}

/** A portal's visitors' conversations, to read (#205). */
function PortalConversations({ portal }: { portal: Portal }) {
  const { t } = useTranslation('portals')
  const [open, setOpen] = useState<string | null>(null)
  const list = useQuery({ queryKey: ['portal-conversations', portal.id], queryFn: () => api.listPortalConversations(portal.id), retry: false })
  const messages = useQuery({
    queryKey: ['portal-messages-admin', portal.id, open],
    queryFn: () => api.getPortalMessages(portal.id, open ?? ''),
    enabled: open !== null,
    retry: false,
  })
  const items = list.data ?? []
  return (
    <div className="space-y-2 rounded-lg border border-line/60 p-3">
      <p className="text-xs text-ink-faint">
        {portal.access === 'members' ? t('conversations.members') : t('conversations.hint')}
      </p>
      {list.isError ? <LoadError compact error={list.error} onRetry={() => void list.refetch()} retrying={list.isFetching} /> : null}
      {list.isLoading ? <p className="text-sm text-ink-faint">…</p> : null}
      {list.isSuccess && items.length === 0 ? <p className="text-sm text-ink-faint">{t('conversations.none')}</p> : null}
      <ul className="divide-y divide-line">
        {items.map((c) => (
          <li key={c.id} className="py-1.5">
            <button
              type="button"
              className="flex w-full items-baseline gap-2 text-start text-sm hover:text-primary"
              aria-expanded={open === c.id}
              onClick={() => setOpen(open === c.id ? null : c.id)}
            >
              <span className="min-w-0 flex-1 truncate text-ink">{c.title || t('conversations.untitled')}</span>
              <span className="shrink-0 text-xs text-ink-muted">
                {c.visitor} · {formatDate(c.updated_at, { dateStyle: 'medium', timeStyle: 'short' })}
              </span>
            </button>
            {open === c.id ? (
              <div className="mt-2 space-y-2 rounded-md bg-ink/5 p-2">
                {messages.isLoading ? <p className="text-sm text-ink-faint">…</p> : null}
                {messages.isError ? (
                  <LoadError compact error={messages.error} onRetry={() => void messages.refetch()} retrying={messages.isFetching} />
                ) : null}
                {(messages.data ?? []).map((m, i) => (
                  <div key={i} className={m.role === 'user' ? 'text-sm text-ink' : 'text-sm text-ink-muted'}>
                    <span className="label-caps me-2">{m.role === 'user' ? c.visitor : t('conversations.assistant')}</span>
                    <span className="whitespace-pre-wrap">{m.content}</span>
                  </div>
                ))}
              </div>
            ) : null}
          </li>
        ))}
      </ul>
    </div>
  )
}

function PortalRow({ portal, onEdit }: { portal: Portal; onEdit: () => void }) {
  const { t } = useTranslation('portals')
  const queryClient = useQueryClient()
  const [reading, setReading] = useState(false)
  const usage = useQuery({ queryKey: ['portal-usage', portal.id], queryFn: () => api.getPortalUsage(portal.id), retry: false })
  const change = useMutation({
    mutationFn: (enabled: boolean) => api.updatePortal(portal.id, { enabled }),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ['portals'] }),
  })
  const remove = useMutation({
    mutationFn: () => api.deletePortal(portal.id),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ['portals'] }),
  })
  return (
    <li className="space-y-2 py-3">
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
        <div className="min-w-0 flex-1">
          <p className="font-medium text-ink">
            {portal.name}
            {!portal.enabled ? <span className="status-chip ms-2 bg-ink/10 text-ink-muted">{t('off')}</span> : null}
          </p>
          <p className="text-xs text-ink-muted">
            {t(`access.${portal.access}`)} · {t(`tools.${portal.tools}`)}
          </p>
          {usage.data ? (
            <p className="text-xs text-ink-faint">
              {t('usage.month', { visitors: usage.data.visitors_30, messages: usage.data.messages_30 })}
            </p>
          ) : null}
        </div>
        <Toggle label={t('enabled', { name: portal.name })} checked={portal.enabled} disabled={change.isPending} onChange={() => change.mutate(!portal.enabled)} />
        <button type="button" className="btn-secondary btn-sm" aria-expanded={reading} onClick={() => setReading(!reading)}>
          {t('conversations.open')}
        </button>
        <button type="button" className="btn-secondary btn-sm" onClick={onEdit}>
          {t('edit.open')}
        </button>
        <button
          type="button"
          className="btn-secondary btn-sm"
          disabled={remove.isPending}
          onClick={() => {
            if (window.confirm(t('remove.confirm', { name: portal.name }))) remove.mutate()
          }}
        >
          {t('remove.button')}
        </button>
      </div>
      {portal.enabled ? <PortalLink slug={portal.slug} /> : null}
      {reading ? <PortalConversations portal={portal} /> : null}
      {change.error || remove.error ? <ShownError {...shownError(change.error ?? remove.error)} className="text-xs text-danger" /> : null}
    </li>
  )
}

/**
 * Administer → Portals (#205): branded chat pages for the people an Admin
 * serves. Add one, choose how it answers and who may use it, see how it
 * looks while editing, copy its link, and turn it off at once.
 */
export function PortalsPage() {
  const { t } = useTranslation('portals')
  const portals = useQuery({ queryKey: ['portals'], queryFn: () => api.listPortals() })
  const [editing, setEditing] = useState<string | null>(null)
  const list = portals.data ?? []
  const current = list.find((p) => p.id === editing)

  return (
    <div className="mx-auto w-full max-w-5xl min-w-0 space-y-6">
      <header className="page-header">
        <RealmKicker />
        <h1 className="page-title">{t('nav.portals', { ns: 'common' })}</h1>
        <p className="page-subtitle">{t('subtitle')}</p>
      </header>

      {current ? <PortalEditor key={current.id} portal={current} onClose={() => setEditing(null)} /> : null}

      <AddPortal onAdded={(p) => setEditing(p.id)} />

      <section className="card" aria-labelledby="all-portals">
        <h2 id="all-portals" className="section-title">
          {t('list.title')}
        </h2>
        {portals.isLoading ? <Skeleton label={t('list.title')} shape="rows" count={2} /> : null}
        {portals.isError ? (
          <LoadError compact error={portals.error} onRetry={() => void portals.refetch()} retrying={portals.isFetching} />
        ) : null}
        {portals.isSuccess && list.length === 0 ? <p className="mt-2 text-sm text-ink-faint">{t('list.none')}</p> : null}
        <ul className="divide-y divide-line">
          {list.map((p) => (
            <PortalRow key={p.id} portal={p} onEdit={() => setEditing(p.id)} />
          ))}
        </ul>
        <p className="mt-3 text-xs text-ink-faint">{t('list.private')}</p>
      </section>
    </div>
  )
}
