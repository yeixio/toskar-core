import { useMutation, useQuery } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ChatMarkdown } from '@/features/chat/ChatMarkdown'
import i18n, { resolveLanguage, systemLanguages } from '@/i18n'
import { api, ApiError, errorText, setPortal, setPortalSession, streamChat } from '@/lib/api'
import type { PortalPageView } from '@/types/api'
import { prompts, safeLogo, text, theme, themeVars } from './branding'

type Turn = { role: 'user' | 'assistant'; content: string }

function conversationKey(slug: string) {
  return `toskar.portal.${slug}.conversation`
}

function savedConversation(slug: string): string {
  try {
    return localStorage.getItem(conversationKey(slug)) ?? ''
  } catch {
    return ''
  }
}

function saveConversation(slug: string, id: string) {
  try {
    if (id) localStorage.setItem(conversationKey(slug), id)
    else localStorage.removeItem(conversationKey(slug))
  } catch {
    // Without storage, the chat lasts until the page closes.
  }
}

/** Whether the page is in another website's frame (#205). */
function framed(): boolean {
  try {
    return window.self !== window.top
  } catch {
    return true
  }
}

function sessionKey(slug: string) {
  return `toskar.portal.${slug}.session`
}

function savedSession(slug: string): string {
  try {
    return sessionStorage.getItem(sessionKey(slug)) ?? ''
  } catch {
    return ''
  }
}

function saveSession(slug: string, token: string) {
  try {
    sessionStorage.setItem(sessionKey(slug), token)
  } catch {
    // Without storage, the visitor enters again after a reload.
  }
}

function usePrefersDark(): boolean {
  const query = typeof window !== 'undefined' && window.matchMedia ? window.matchMedia('(prefers-color-scheme: dark)') : null
  const [dark, setDark] = useState(query?.matches ?? true)
  useEffect(() => {
    if (!query) return
    const on = (e: MediaQueryListEvent) => setDark(e.matches)
    query.addEventListener('change', on)
    return () => query.removeEventListener('change', on)
  }, [query])
  return dark
}

/**
 * A chat portal's page (#205): only a chat, with the portal's name, logo,
 * colours, and welcome, and nothing of the app around it. A visitor enters
 * (with the passcode, when it has one) and chats as the portal's guest.
 */
export function PortalPage({ slug }: { slug: string }) {
  const { t } = useTranslation('portal')
  const prefersDark = usePrefersDark()
  const [entered, setEntered] = useState(false)

  // Every request from here is the portal's; leaving goes back to the app.
  // In another website's frame, the guest's session goes in a header.
  const embedded = framed()
  useEffect(() => {
    setPortal(slug)
    if (embedded) setPortalSession(savedSession(slug))
    return () => setPortal('')
  }, [slug, embedded])

  const page = useQuery({ queryKey: ['portal', slug], queryFn: () => api.getPortalPage(slug), retry: false })
  const view = page.data

  // The portal's language, or the visitor's own, without changing the
  // app's in this browser.
  useEffect(() => {
    if (!view) return
    void i18n.changeLanguage(resolveLanguage(view.language, systemLanguages()))
  }, [view])

  const enter = useMutation({
    mutationFn: ({ passcode = '', invite = '' }: { passcode?: string; invite?: string }) => api.enterPortal(slug, passcode, invite, embedded),
    onSuccess: (guest) => {
      if (embedded && guest?.session) {
        saveSession(slug, guest.session)
        setPortalSession(guest.session)
      }
      setEntered(true)
    },
  })
  // An open portal lets anyone in at once, and an invitation link its
  // visitor; the link's one-time token leaves the address once used.
  const enterNow = enter.mutate
  useEffect(() => {
    if (!view) return
    const invite = new URLSearchParams(window.location.search).get('invite') ?? ''
    if (invite) window.history.replaceState(null, '', window.location.pathname)
    if (view.entered) setEntered(true)
    else if (view.access === 'open') enterNow({})
    else if (view.access === 'invited' && invite) enterNow({ invite })
  }, [view, enterNow])
  const refusal = enter.error instanceof ApiError && enter.error.code ? errorText(enter.error.code, enter.error.message) : ''

  const branding = view?.branding ?? {}
  const title = text(branding.title, view?.name ?? '', 80)
  const logo = safeLogo(branding.logo_url)

  return (
    <div
      data-theme={theme(branding, prefersDark)}
      style={themeVars(branding)}
      className="flex h-full min-h-0 flex-col bg-canvas text-ink"
    >
      {page.isLoading ? (
        <div className="flex flex-1 items-center justify-center" role="status" aria-busy="true">
          <span className="inline-block h-6 w-6 animate-spin rounded-full border-2 border-line border-t-primary" />
        </div>
      ) : page.isError && !(page.error instanceof ApiError && page.error.status >= 400 && page.error.status < 500) ? (
        // A failed request isn't a missing portal: the visitor can try again (#217).
        <div className="flex flex-1 flex-col items-center justify-center gap-4 px-6 text-center" role="alert">
          <p className="text-sm text-ink-muted">{t('error')}</p>
          <button type="button" className="btn-secondary" disabled={page.isFetching} onClick={() => void page.refetch()}>
            {t('loadError.retry', { ns: 'common' })}
          </button>
        </div>
      ) : !view ? (
        <div className="flex flex-1 flex-col items-center justify-center px-6 text-center">
          <h1 className="font-display text-2xl font-semibold">{t('gone.title')}</h1>
          <p className="mt-2 max-w-md text-sm text-ink-muted">{t('gone.body')}</p>
        </div>
      ) : (
        <>
          <header className="flex items-center gap-3 border-b border-line/60 px-4 py-3">
            {logo ? <img src={logo} alt="" className="h-8 w-8 rounded-md object-contain" /> : null}
            <h1 className="min-w-0 flex-1 truncate font-display text-lg font-semibold">{title}</h1>
          </header>
          {entered ? (
            <PortalChat slug={slug} view={view} onLeft={() => setEntered(false)} />
          ) : view.access === 'passcode' ? (
            <PasscodeForm
              name={title}
              busy={enter.isPending}
              error={enter.error instanceof ApiError && enter.error.status === 401 ? t('enter.wrong') : enter.error ? t('error') : ''}
              onSubmit={(code) => enter.mutate({ passcode: code })}
            />
          ) : view.access === 'members' ? (
            <div className="mx-auto flex w-full max-w-sm flex-1 flex-col justify-center gap-3 px-6 text-center">
              <p className="text-sm text-ink-muted">{t('members.body', { name: title })}</p>
              <a className="btn-primary" href={`/?next=${encodeURIComponent(`/p/${slug}`)}`}>
                {t('members.signIn')}
              </a>
            </div>
          ) : view.access === 'invited' && !enter.isPending ? (
            <div className="mx-auto flex w-full max-w-sm flex-1 flex-col justify-center px-6 text-center">
              <p className="text-sm text-ink-muted" role={refusal ? 'alert' : undefined}>
                {refusal || t('invited.body', { name: title })}
              </p>
            </div>
          ) : enter.isError ? (
            <div className="m-6 flex flex-wrap items-center gap-3" role="alert">
              <p className="text-sm text-danger">{t('error')}</p>
              <button type="button" className="btn-secondary btn-sm" disabled={enter.isPending} onClick={() => enter.mutate(enter.variables ?? {})}>
                {t('loadError.retry', { ns: 'common' })}
              </button>
            </div>
          ) : enter.isPending ? (
            <div className="flex flex-1 items-center justify-center" role="status" aria-busy="true">
              <span className="inline-block h-6 w-6 animate-spin rounded-full border-2 border-line border-t-primary" />
            </div>
          ) : null}
          {text(branding.footer) ? (
            <footer className="border-t border-line/60 px-4 py-2 text-center text-xs text-ink-faint">{text(branding.footer)}</footer>
          ) : null}
        </>
      )}
    </div>
  )
}

function PasscodeForm({ name, busy, error, onSubmit }: { name: string; busy: boolean; error: string; onSubmit: (code: string) => void }) {
  const { t } = useTranslation('portal')
  const [code, setCode] = useState('')
  return (
    <form
      className="mx-auto flex w-full max-w-sm flex-1 flex-col justify-center gap-3 px-6"
      onSubmit={(e) => {
        e.preventDefault()
        if (code.trim()) onSubmit(code.trim())
      }}
    >
      <p className="text-center text-sm text-ink-muted">{t('enter.body', { name })}</p>
      <label className="space-y-1 text-sm">
        <span className="text-ink-muted">{t('enter.passcode')}</span>
        <input className="field w-full" type="password" autoComplete="off" value={code} onChange={(e) => setCode(e.target.value)} />
      </label>
      {error ? (
        <p className="text-sm text-danger" role="alert">
          {error}
        </p>
      ) : null}
      <button type="submit" className="btn-primary" disabled={busy || !code.trim()}>
        {t('enter.submit')}
      </button>
    </form>
  )
}

function PortalChat({ slug, view, onLeft }: { slug: string; view: PortalPageView; onLeft: () => void }) {
  const { t } = useTranslation('portal')
  const [conversationId, setConversationId] = useState(() => savedConversation(slug))
  // The chat from an earlier visit, shown once; a chat started here is
  // already on screen.
  const [restoreId, setRestoreId] = useState(conversationId)
  const [turns, setTurns] = useState<Turn[]>([])
  const [draft, setDraft] = useState('')
  const [sending, setSending] = useState(false)
  const [error, setError] = useState('')
  const abort = useRef<AbortController | null>(null)
  const bottom = useRef<HTMLDivElement | null>(null)
  const branding = view.branding ?? {}
  const suggestions = prompts(branding)

  // The chat this browser had here before, if it's still theirs: it may
  // have gone with the portal's retention, or be from another portal that
  // had this address, and a message to it would be lost.
  const mine = useQuery({
    queryKey: ['portal-own-conversations', slug],
    queryFn: () => api.getConversations(),
    enabled: restoreId !== '',
    retry: false,
  })
  const stillMine = mine.isSuccess && (mine.data ?? []).some((c) => c.id === restoreId)
  useEffect(() => {
    if (restoreId && mine.isSuccess && !stillMine) {
      setConversationId('')
      setRestoreId('')
      saveConversation(slug, '')
    }
  }, [restoreId, mine.isSuccess, stillMine, slug])
  const history = useQuery({
    queryKey: ['portal-messages', slug, restoreId],
    queryFn: () => api.getMessages(restoreId),
    enabled: restoreId !== '' && stillMine,
    retry: false,
  })
  useEffect(() => {
    if (history.data) {
      setTurns(
        history.data
          .filter((m) => m.role === 'user' || m.role === 'assistant')
          .map((m) => ({ role: m.role as Turn['role'], content: m.content })),
      )
    } else if (history.isError) {
      if (history.error instanceof ApiError && history.error.status === 401) onLeft()
      setConversationId('')
      setRestoreId('')
      saveConversation(slug, '')
    }
  }, [history.data, history.isError, history.error, slug, onLeft])

  useEffect(() => {
    bottom.current?.scrollIntoView?.({ block: 'end' })
  }, [turns])

  const send = async (message: string) => {
    const words = message.trim()
    if (!words || sending) return
    setError('')
    setDraft('')
    setSending(true)
    setTurns((cur) => [...cur, { role: 'user', content: words }, { role: 'assistant', content: '' }])
    try {
      let id = conversationId
      if (!id) {
        const created = await api.createConversation({ title: words.length > 48 ? `${words.slice(0, 48)}…` : words, profile_id: '', model_id: '' })
        id = created?.id ?? ''
        setConversationId(id)
        saveConversation(slug, id)
      }
      const controller = new AbortController()
      abort.current = controller
      await streamChat({
        body: { conversation_id: id, message: words },
        signal: controller.signal,
        onToken: (piece) =>
          setTurns((cur) => {
            const last = cur[cur.length - 1]
            if (last?.role !== 'assistant') return [...cur, { role: 'assistant', content: piece }]
            return [...cur.slice(0, -1), { ...last, content: last.content + piece }]
          }),
        // A limit says which, in the visitor's language (#205).
        onError: (message, code) => setError(code?.startsWith('PORTAL_') ? errorText(code, message, { max: view.max_message }) : t('error')),
      })
    } catch (err) {
      if (err instanceof ApiError && err.code?.startsWith('PORTAL_')) setError(errorText(err.code, err.message, { max: view.max_message, ...err.details }))
      else if (!(err instanceof DOMException && err.name === 'AbortError')) setError(t('error'))
    } finally {
      abort.current = null
      setSending(false)
      // A reply that never came, such as one a limit stopped, leaves no
      // empty bubble behind.
      setTurns((cur) => {
        const last = cur[cur.length - 1]
        return last?.role === 'assistant' && last.content === '' ? cur.slice(0, -1) : cur
      })
    }
  }

  const stop = () => {
    abort.current?.abort()
    if (conversationId) void api.stopChat(conversationId).catch(() => undefined)
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="min-h-0 flex-1 overflow-y-auto px-4 py-6">
        <div className="mx-auto flex w-full max-w-2xl flex-col gap-4">
          {turns.length === 0 && restoreId && (mine.isLoading || history.isLoading) ? (
            <div className="flex justify-center py-10" role="status" aria-busy="true">
              <span className="inline-block h-6 w-6 animate-spin rounded-full border-2 border-line border-t-primary" />
            </div>
          ) : turns.length === 0 ? (
            <div className="space-y-4 py-10 text-center">
              <p className="text-ink-muted">{text(branding.welcome, t('welcome', { name: view.name }), 1000)}</p>
              {suggestions.length > 0 ? (
                <div className="flex flex-wrap justify-center gap-2">
                  {suggestions.map((s) => (
                    <button key={s} type="button" className="btn-secondary btn-sm" onClick={() => void send(s)}>
                      {s}
                    </button>
                  ))}
                </div>
              ) : null}
            </div>
          ) : (
            turns.map((turn, i) =>
              turn.role === 'user' ? (
                <div key={i} className="self-end rounded-2xl bg-primary px-4 py-2 text-sm text-primary-fg">
                  {turn.content}
                </div>
              ) : (
                <div key={i} className="text-sm" aria-live={i === turns.length - 1 ? 'polite' : undefined}>
                  {turn.content ? <ChatMarkdown text={turn.content} /> : <span className="text-ink-faint">{t('thinking')}</span>}
                </div>
              ),
            )
          )}
          {error ? (
            <p className="text-sm text-danger" role="alert">
              {error}
            </p>
          ) : null}
          <div ref={bottom} />
        </div>
      </div>
      <form
        className="border-t border-line/60 px-4 py-3"
        onSubmit={(e) => {
          e.preventDefault()
          void send(draft)
        }}
      >
        <div className="mx-auto flex w-full max-w-2xl items-end gap-2">
          <textarea
            className="field min-h-[2.75rem] flex-1 resize-none"
            rows={1}
            maxLength={view.max_message || undefined}
            aria-label={t('message', { name: view.name })}
            placeholder={t('message', { name: view.name })}
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && !e.shiftKey) {
                e.preventDefault()
                void send(draft)
              }
            }}
          />
          {sending ? (
            <button type="button" className="btn-secondary" onClick={stop}>
              {t('stop')}
            </button>
          ) : (
            <button type="submit" className="btn-primary" disabled={!draft.trim()}>
              {t('send')}
            </button>
          )}
        </div>
        {view.access !== 'members' ? (
          // Visitors are told the people who run the portal can read their
          // chats (#205).
          <p className="mx-auto mt-2 w-full max-w-2xl text-center text-[11px] text-ink-faint">{t('notice', { name: view.name })}</p>
        ) : null}
        {turns.length > 0 && !sending ? (
          <div className="mx-auto mt-2 flex w-full max-w-2xl justify-end">
            <button
              type="button"
              className="text-xs text-ink-muted hover:text-ink"
              onClick={() => {
                setTurns([])
                setConversationId('')
                setRestoreId('')
                saveConversation(slug, '')
              }}
            >
              {t('newChat')}
            </button>
          </div>
        ) : null}
      </form>
    </div>
  )
}
