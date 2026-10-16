import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate, useParams } from 'react-router-dom'
import { YggdrasilMark } from '@/components/ui/YggdrasilMark'
import { api, forgetApiKey } from '@/lib/api'
import { useUIStore } from '@/stores/uiStore'
import { LoadError } from '@/components/ui/LoadError'

/**
 * The page a one-time link opens (#206): the person chooses a username and
 * password, or a new password, and is signed in. It answers before
 * sign-in, so it sits outside the app's gates.
 */
export function InvitePage() {
  const { t } = useTranslation('people')
  const { token = '' } = useParams()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const setOnboardingComplete = useUIStore((s) => s.setOnboardingComplete)
  const invite = useQuery({ queryKey: ['invite', token], queryFn: () => api.peekInvite(token), retry: false })
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [again, setAgain] = useState('')
  const [error, setError] = useState('')
  const reset = invite.data?.kind === 'reset'
  const accept = useMutation({
    mutationFn: () => api.acceptInvite(token, reset ? invite.data?.username ?? '' : username.trim(), password),
    onSuccess: async () => {
      // Signed in by cookie: a key stored here from before would speak
      // for someone else, and this person joins a Toskar already set up.
      forgetApiKey()
      setOnboardingComplete(true)
      // The link is used up; asking about it again would only fail.
      queryClient.removeQueries({ queryKey: ['invite', token] })
      await queryClient.invalidateQueries()
      navigate('/chat', { replace: true })
    },
    onError: (err) => setError(err instanceof Error ? err.message : String(err)),
  })

  return (
    <div className="flex h-full min-h-0 flex-col items-center justify-center overflow-auto bg-canvas px-6 py-10 text-center">
      <YggdrasilMark size={64} lore />
      {invite.isLoading ? (
        <p className="mt-6 text-sm text-ink-muted">{t('invite.loading')}</p>
      ) : invite.error && !refused(invite.error) ? (
        // A failed request isn't an expired link: say so, with Retry (#217).
        <div className="mt-6 w-full max-w-md">
          <LoadError error={invite.error} onRetry={() => void invite.refetch()} retrying={invite.isFetching} />
        </div>
      ) : invite.error || !invite.data ? (
        <>
          <h1 className="mt-6 font-display text-2xl font-semibold tracking-tight text-ink">{t('invite.goneTitle')}</h1>
          <p className="mt-2 max-w-md text-sm leading-relaxed text-ink-muted">{t('invite.goneBody')}</p>
        </>
      ) : (
        <form
          className="mt-6 flex w-full max-w-sm flex-col gap-3 text-start"
          onSubmit={(e) => {
            e.preventDefault()
            setError('')
            if (password !== again) {
              setError(t('invite.mismatch'))
              return
            }
            accept.mutate()
          }}
        >
          <h1 className="text-center font-display text-2xl font-semibold tracking-tight text-ink">
            {reset ? t('invite.resetTitle', { name: invite.data.name }) : t('invite.title', { name: invite.data.name })}
          </h1>
          <p className="text-center text-sm leading-relaxed text-ink-muted">{reset ? t('invite.resetBody') : t('invite.body')}</p>
          {reset ? (
            <p className="text-center text-sm text-ink">@{invite.data.username}</p>
          ) : (
            <label className="space-y-1 text-sm">
              <span className="text-ink-muted">{t('signIn.username')}</span>
              <input
                className="field w-full"
                autoComplete="username"
                autoCapitalize="none"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                required
              />
              <span className="text-xs text-ink-faint">{t('invite.usernameHint')}</span>
            </label>
          )}
          <label className="space-y-1 text-sm">
            <span className="text-ink-muted">{t('invite.password')}</span>
            <input
              className="field w-full"
              type="password"
              autoComplete="new-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
            />
            <span className="text-xs text-ink-faint">{t('invite.passwordHint')}</span>
          </label>
          <label className="space-y-1 text-sm">
            <span className="text-ink-muted">{t('invite.again')}</span>
            <input
              className="field w-full"
              type="password"
              autoComplete="new-password"
              value={again}
              onChange={(e) => setAgain(e.target.value)}
              required
            />
          </label>
          {error ? (
            <p className="text-sm text-danger" role="alert">
              {error}
            </p>
          ) : null}
          <button type="submit" className="btn-primary mt-1" disabled={accept.isPending || !password || (!reset && !username.trim())}>
            {reset ? t('invite.resetSubmit') : t('invite.submit')}
          </button>
        </form>
      )}
    </div>
  )
}

/** Toskar answered and turned the link down (4xx), as opposed to not answering. */
function refused(error: unknown): boolean {
  const status = (error as { status?: unknown } | null)?.status
  return typeof status === 'number' && status >= 400 && status < 500
}
