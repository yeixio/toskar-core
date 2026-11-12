import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api } from '@/lib/api'
import { useRole } from '@/lib/role'
import { useDialog } from '@/lib/useDialog'
import { MAX_UPLOAD_BYTES, UPLOAD_ACCEPT, readUpload } from '@/lib/upload'
import { formatRelativeTime, formatSize } from '@/i18n/format'
import { RealmKicker } from '@/components/ui/Realm'
import { Toggle } from '@/components/ui/Toggle'
import { LoadError } from '@/components/ui/LoadError'
import { Skeleton } from '@/components/ui/Skeleton'
import { ShownError, shownError } from '@/components/ui/ShownError'
import { KnowledgePicker } from '@/features/knowledge/KnowledgePicker'
import { canChat } from '@/features/models/modelPresentation'
import { sortProfilesForDisplay } from '@/features/profiles/profilePresentation'
import type { Project, ProjectInput } from '@/types/api'

/** The model id that lets Toskar choose per message. */
const AUTO_MODEL_ID = 'auto'

/**
 * A project's page (#1136): its chats with New chat, and what they share:
 * instructions, files and linked knowledge, a folder (Admins), a default
 * profile and model, and the project's memory. Every change saves on its
 * own, as a chat's options do.
 */
export function ProjectPage() {
  const { t } = useTranslation('projects')
  const { id = '' } = useParams()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  // Until the role is known, the Owner's view, as Chat does.
  const { role, atLeast } = useRole()
  const admin = role === undefined || atLeast('admin')
  const member = role === undefined || atLeast('member')
  const project = useQuery({ queryKey: ['project', id], queryFn: () => api.getProject(id), retry: false })
  const chats = useQuery({
    queryKey: ['conversations', 'project', id],
    queryFn: () => api.getProjectConversations(id),
    retry: false,
  })
  const update = useMutation({
    mutationFn: (body: ProjectInput) => api.updateProject(id, body),
    onSuccess: (next) => {
      if (next) queryClient.setQueryData(['project', id], next)
      void queryClient.invalidateQueries({ queryKey: ['projects'] })
    },
  })
  const [deleting, setDeleting] = useState(false)

  if (project.isLoading) return <Skeleton label={t('page.loading')} />
  if (project.isError) {
    return (
      <div className="page-fill p-4 sm:p-6">
        <LoadError error={project.error} onRetry={() => void project.refetch()} retrying={project.isFetching} />
      </div>
    )
  }
  const p = project.data
  if (!p) {
    return (
      <div className="page-fill gap-3 p-4 sm:p-6">
        <p className="text-sm text-ink-muted" role="status">
          {t('page.notFound')}
        </p>
        <Link to="/chat" className="text-sm font-medium text-primary-active hover:underline">
          {t('page.back')}
        </Link>
      </div>
    )
  }

  return (
    <div className="page-fill gap-5 overflow-y-auto p-4 sm:p-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div className="min-w-0 max-w-2xl">
          <RealmKicker path="/chat" />
          <Link to="/chat" className="text-xs font-medium text-ink-muted hover:text-ink hover:underline">
            {t('page.back')}
          </Link>
          <ProjectName project={p} saving={update.isPending} onSave={(name) => update.mutateAsync({ name })} />
          <p className="page-subtitle mt-1">{t('page.description')}</p>
        </div>
        <button
          type="button"
          className="btn-primary"
          onClick={() => navigate(`/chat?new=1&project=${encodeURIComponent(p.id)}`)}
        >
          {t('page.newChat')}
        </button>
      </div>
      {update.error ? <ShownError {...shownError(update.error)} role="alert" /> : null}

      <div className="grid gap-5 lg:grid-cols-2">
        <div className="min-w-0 space-y-5">
          <Card title={t('chats.title')}>
            {chats.isError ? (
              <LoadError compact error={chats.error} onRetry={() => void chats.refetch()} retrying={chats.isFetching} />
            ) : chats.isLoading ? (
              <Skeleton label={t('chats.loading')} />
            ) : (chats.data ?? []).length === 0 ? (
              <p className="text-sm text-ink-muted">{t('chats.empty')}</p>
            ) : (
              <ul className="-mx-2 space-y-0.5">
                {(chats.data ?? []).map((chat) => (
                  <li key={chat.id}>
                    <Link
                      to={`/chat?c=${encodeURIComponent(chat.id)}`}
                      className="flex items-baseline gap-3 rounded-lg px-2 py-1.5 text-sm text-ink hover:bg-raised"
                    >
                      <span className="min-w-0 flex-1 truncate">{chat.title || t('chats.untitled')}</span>
                      <span className="shrink-0 text-xs text-ink-faint">{formatRelativeTime(chat.updated_at || chat.created_at)}</span>
                    </Link>
                  </li>
                ))}
              </ul>
            )}
          </Card>
          <Instructions project={p} saving={update.isPending} onSave={(instructions) => update.mutate({ instructions })} />
          <Files project={p} />
        </div>
        <div className="min-w-0 space-y-5">
          <Defaults project={p} disabled={update.isPending} onChange={(body) => update.mutate(body)} />
          <Card title={t('memory.title')}>
            <div className="flex items-start justify-between gap-4">
              <p className="text-sm text-ink-muted">{t('memory.hint')}</p>
              <Toggle
                checked={!p.memory_off}
                disabled={update.isPending}
                label={t('memory.toggle')}
                onChange={() => update.mutate({ memory_off: !p.memory_off })}
              />
            </div>
          </Card>
          {/* A folder gives the project's chats tools there, so it's an Admin's to set. */}
          {admin ? <Folder project={p} /> : null}
          {/* Linking knowledge is for Members and up; connecting a new source, for Admins. */}
          {member ? (
            <Card title={t('knowledge.title')} hint={t('knowledge.hint')}>
              <KnowledgePicker
                selected={p.knowledge ?? []}
                disabled={update.isPending}
                canConnect={admin}
                onChange={(knowledge) => update.mutate({ knowledge })}
              />
            </Card>
          ) : null}
          <Card title={t('delete.title')} hint={t('delete.hint')}>
            <button type="button" className="btn-danger" onClick={() => setDeleting(true)}>
              {t('delete.button')}
            </button>
          </Card>
        </div>
      </div>
      {deleting ? <DeleteProjectDialog project={p} onClose={() => setDeleting(false)} onDeleted={() => navigate('/chat')} /> : null}
    </div>
  )
}

function Card({ title, hint, children }: { title: string; hint?: string; children: ReactNode }) {
  return (
    <section className="card space-y-3">
      <div>
        <h2 className="font-display text-base font-semibold text-ink">{title}</h2>
        {hint ? <p className="mt-1 text-sm text-ink-muted">{hint}</p> : null}
      </div>
      {children}
    </section>
  )
}

function ProjectName({ project, saving, onSave }: { project: Project; saving: boolean; onSave: (name: string) => Promise<unknown> }) {
  const { t } = useTranslation('projects')
  const [editing, setEditing] = useState(false)
  const [name, setName] = useState(project.name)
  if (!editing) {
    return (
      <div className="flex min-w-0 items-baseline gap-3">
        <h1 className="page-title min-w-0 truncate">{project.name}</h1>
        <button
          type="button"
          className="shrink-0 text-xs font-medium text-ink-muted hover:text-ink hover:underline"
          onClick={() => {
            setName(project.name)
            setEditing(true)
          }}
        >
          {t('page.rename')}
        </button>
      </div>
    )
  }
  return (
    <form
      className="mt-1 flex flex-wrap items-center gap-2"
      onSubmit={(event) => {
        event.preventDefault()
        if (!name.trim()) return
        void onSave(name.trim()).then(() => setEditing(false), () => undefined)
      }}
    >
      <input
        className="field min-w-[12rem] flex-1"
        value={name}
        maxLength={200}
        autoFocus
        aria-label={t('page.name')}
        onChange={(event) => setName(event.target.value)}
        onKeyDown={(event) => {
          if (event.key === 'Escape') setEditing(false)
        }}
      />
      <button type="submit" className="btn-primary" disabled={saving || !name.trim()}>
        {t('page.save')}
      </button>
      <button type="button" className="btn-secondary" onClick={() => setEditing(false)}>
        {t('page.cancel')}
      </button>
    </form>
  )
}

function Instructions({ project, saving, onSave }: { project: Project; saving: boolean; onSave: (instructions: string) => void }) {
  const { t } = useTranslation('projects')
  const [draft, setDraft] = useState(project.instructions)
  // A save, or another window's change, replaces the draft once it's saved.
  useEffect(() => setDraft(project.instructions), [project.instructions])
  const changed = draft !== project.instructions
  return (
    <Card title={t('instructions.title')} hint={t('instructions.hint')}>
      <textarea
        className="field min-h-32 w-full text-sm leading-relaxed"
        value={draft}
        aria-label={t('instructions.title')}
        placeholder={t('instructions.placeholder')}
        onChange={(event) => setDraft(event.target.value)}
      />
      <div className="flex gap-2">
        <button type="button" className="btn-primary text-sm" disabled={!changed || saving} onClick={() => onSave(draft)}>
          {t('instructions.save')}
        </button>
        {changed ? (
          <button type="button" className="btn-secondary text-sm" onClick={() => setDraft(project.instructions)}>
            {t('instructions.discard')}
          </button>
        ) : null}
      </div>
    </Card>
  )
}

function Files({ project }: { project: Project }) {
  const { t } = useTranslation('projects')
  const queryClient = useQueryClient()
  const inputRef = useRef<HTMLInputElement>(null)
  const [tooBig, setTooBig] = useState<string | null>(null)
  const saved = (next: Project | null) => {
    if (next) queryClient.setQueryData(['project', project.id], next)
    else void queryClient.invalidateQueries({ queryKey: ['project', project.id] })
  }
  const add = useMutation({
    mutationFn: async (file: File) => api.addProjectFile(project.id, await readUpload(file)),
    onSuccess: saved,
  })
  const remove = useMutation({
    mutationFn: (name: string) => api.removeProjectFile(project.id, name),
    onSuccess: () => saved(null),
  })
  const files = project.files ?? []
  return (
    <Card title={t('files.title')} hint={t('files.hint')}>
      {files.length === 0 ? (
        <p className="text-sm text-ink-muted">{t('files.empty')}</p>
      ) : (
        <ul className="space-y-1">
          {files.map((file) => (
            <li key={file.name} className="flex items-center gap-2 text-sm">
              <span className="min-w-0 flex-1 truncate text-ink" title={file.name}>
                {file.name} <span className="text-xs text-ink-faint">· {formatSize(file.size)}</span>
              </span>
              <button
                type="button"
                className="btn-secondary px-2 py-0.5 text-xs"
                disabled={remove.isPending}
                aria-label={t('files.removeName', { name: file.name })}
                onClick={() => remove.mutate(file.name)}
              >
                {t('files.remove')}
              </button>
            </li>
          ))}
        </ul>
      )}
      <input
        ref={inputRef}
        type="file"
        className="hidden"
        accept={UPLOAD_ACCEPT}
        aria-label={t('files.add')}
        onChange={(event) => {
          const file = event.target.files?.[0]
          event.target.value = ''
          if (!file) return
          if (file.size > MAX_UPLOAD_BYTES) {
            setTooBig(file.name)
            return
          }
          setTooBig(null)
          add.mutate(file)
        }}
      />
      <button type="button" className="btn-secondary text-sm" disabled={add.isPending} onClick={() => inputRef.current?.click()}>
        {add.isPending ? t('files.adding') : t('files.add')}
      </button>
      {tooBig ? (
        <p className="text-sm text-danger" role="alert">
          {t('files.tooBig', { name: tooBig, size: formatSize(MAX_UPLOAD_BYTES) })}
        </p>
      ) : null}
      {add.error ? <ShownError {...shownError(add.error)} role="alert" /> : null}
      {remove.error ? <ShownError {...shownError(remove.error)} role="alert" /> : null}
    </Card>
  )
}

function Folder({ project }: { project: Project }) {
  const { t } = useTranslation('projects')
  const queryClient = useQueryClient()
  const [value, setValue] = useState(project.folder ?? '')
  useEffect(() => setValue(project.folder ?? ''), [project.folder])
  const save = useMutation({
    mutationFn: (folder: string) => api.updateProject(project.id, { folder }),
    onSuccess: (next) => {
      if (next) queryClient.setQueryData(['project', project.id], next)
    },
  })
  return (
    <Card title={t('folder.title')} hint={t('folder.hint')}>
      <form
        className="flex flex-wrap gap-2"
        onSubmit={(event) => {
          event.preventDefault()
          if (value.trim()) save.mutate(value.trim())
        }}
      >
        <input
          className="field min-w-0 flex-1 font-mono text-xs"
          value={value}
          spellCheck={false}
          autoCapitalize="off"
          autoCorrect="off"
          aria-label={t('folder.label')}
          placeholder={t('folder.placeholder')}
          onChange={(event) => setValue(event.target.value)}
        />
        <button type="submit" className="btn-secondary px-3 py-1 text-xs" disabled={save.isPending || !value.trim() || value.trim() === project.folder}>
          {t('folder.save')}
        </button>
        {project.folder ? (
          <button type="button" className="btn-secondary px-3 py-1 text-xs" disabled={save.isPending} onClick={() => save.mutate('')}>
            {t('folder.remove')}
          </button>
        ) : null}
      </form>
      {save.error ? <ShownError {...shownError(save.error)} role="alert" /> : null}
    </Card>
  )
}

function Defaults({ project, disabled, onChange }: { project: Project; disabled: boolean; onChange: (body: ProjectInput) => void }) {
  const { t } = useTranslation('projects')
  const profiles = useQuery({ queryKey: ['profiles'], queryFn: () => api.getProfiles(), retry: false })
  const models = useQuery({ queryKey: ['models'], queryFn: () => api.getModels(), retry: false })
  // A person pinned to some profiles chooses among those (#345).
  const me = useQuery({ queryKey: ['me'], queryFn: () => api.getMe(), staleTime: 60_000, retry: false })
  const allowed = me.data?.profiles
  const profileList = sortProfilesForDisplay(profiles.data ?? []).filter((p) => !allowed || allowed.includes(p.id))
  const modelList = (models.data ?? []).filter((m) => (m.installed || (m.installed_on?.length ?? 0) > 0) && canChat(m))
  const profileId = project.profile_id ?? ''
  const modelId = project.model_id ?? ''
  return (
    <Card title={t('defaults.title')} hint={t('defaults.hint')}>
      <div className="grid gap-3 sm:grid-cols-2">
        <label className="block space-y-1">
          <span className="text-sm text-ink">{t('defaults.profile')}</span>
          <select className="field w-full" value={profileId} disabled={disabled} onChange={(event) => onChange({ profile_id: event.target.value })}>
            <option value="">{t('defaults.yourDefault')}</option>
            {/* Kept when it isn't listed, such as before the profiles load. */}
            {profileId && !profileList.some((p) => p.id === profileId) ? <option value={profileId}>{profileId}</option> : null}
            {profileList.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </select>
        </label>
        <label className="block space-y-1">
          <span className="text-sm text-ink">{t('defaults.model')}</span>
          <select className="field w-full" value={modelId} disabled={disabled} onChange={(event) => onChange({ model_id: event.target.value })}>
            <option value="">{t('defaults.yourDefault')}</option>
            <option value={AUTO_MODEL_ID}>{t('defaults.auto')}</option>
            {modelId && modelId !== AUTO_MODEL_ID && !modelList.some((m) => m.id === modelId) ? <option value={modelId}>{modelId}</option> : null}
            {modelList.map((m) => (
              <option key={m.id} value={m.id}>
                {m.display_name || m.id}
              </option>
            ))}
          </select>
        </label>
      </div>
    </Card>
  )
}

function DeleteProjectDialog({ project, onClose, onDeleted }: { project: Project; onClose: () => void; onDeleted: () => void }) {
  const { t } = useTranslation('projects')
  const queryClient = useQueryClient()
  const ref = useDialog(true, onClose)
  const [deleteChats, setDeleteChats] = useState(false)
  const remove = useMutation({
    mutationFn: () => api.deleteProject(project.id, { deleteChats }),
    onSuccess: () => {
      queryClient.removeQueries({ queryKey: ['project', project.id] })
      void queryClient.invalidateQueries({ queryKey: ['projects'] })
      void queryClient.invalidateQueries({ queryKey: ['conversations'] })
      onDeleted()
    },
  })
  return (
    <div
      ref={ref}
      className="fixed inset-0 z-50 flex items-end justify-center scrim p-4 sm:items-center"
      role="dialog"
      aria-modal="true"
      aria-labelledby="delete-project-title"
    >
      <div className="card w-full max-w-sm space-y-4 shadow-panel">
        <div>
          <h2 id="delete-project-title" className="break-words font-display text-lg font-semibold text-ink">
            {t('delete.confirmTitle', { name: project.name })}
          </h2>
          <p className="mt-1 text-sm text-ink-muted">{deleteChats ? t('delete.bodyChats') : t('delete.body')}</p>
        </div>
        {project.chat_count > 0 ? (
          <label className="flex items-center gap-2 text-sm text-ink">
            <input type="checkbox" checked={deleteChats} onChange={(event) => setDeleteChats(event.target.checked)} />
            {t('delete.alsoChats', { count: project.chat_count })}
          </label>
        ) : null}
        {remove.error ? <ShownError {...shownError(remove.error)} role="alert" /> : null}
        <div className="flex flex-wrap justify-end gap-2">
          <button type="button" className="btn-secondary" data-autofocus onClick={onClose}>
            {t('delete.cancel')}
          </button>
          <button type="button" className="btn-danger" disabled={remove.isPending} onClick={() => remove.mutate()}>
            {t('delete.confirm')}
          </button>
        </div>
      </div>
    </div>
  )
}
