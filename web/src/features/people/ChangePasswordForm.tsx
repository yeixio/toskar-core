import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { YggdrasilMark } from '@/components/ui/YggdrasilMark'
import { api } from '@/lib/api'

/**
 * Changing a password an Admin set (#592): asked for right after signing
 * in, before anything else, since the Admin knows it.
 */
export function ChangePasswordForm({ name, onChanged }: { name: string; onChanged: () => void }) {
  const { t } = useTranslation('people')
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const [again, setAgain] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const mismatch = again !== '' && again !== next
  return (
    <form
      className="flex h-full min-h-0 flex-col items-center justify-center bg-canvas px-6 text-center"
      onSubmit={async (e) => {
        e.preventDefault()
        if (mismatch) return
        setBusy(true)
        setError('')
        try {
          await api.changeMyPassword(current, next)
          setCurrent('')
          setNext('')
          setAgain('')
          onChanged()
        } catch (err) {
          setError(err instanceof Error ? err.message : String(err))
        } finally {
          setBusy(false)
        }
      }}
    >
      <YggdrasilMark size={64} lore />
      <h1 className="mt-6 font-display text-2xl font-semibold tracking-tight text-ink">{t('changePassword.title', { name })}</h1>
      <p className="mt-2 max-w-md text-sm leading-relaxed text-ink-muted">{t('changePassword.body')}</p>
      <div className="mt-6 flex w-full max-w-sm flex-col gap-3 text-start">
        <label className="space-y-1 text-sm">
          <span className="text-ink-muted">{t('changePassword.current')}</span>
          <input className="field w-full" type="password" autoComplete="current-password" value={current} onChange={(e) => setCurrent(e.target.value)} />
        </label>
        <label className="space-y-1 text-sm">
          <span className="text-ink-muted">{t('changePassword.new')}</span>
          <input className="field w-full" type="password" autoComplete="new-password" value={next} onChange={(e) => setNext(e.target.value)} />
        </label>
        <label className="space-y-1 text-sm">
          <span className="text-ink-muted">{t('changePassword.again')}</span>
          <input
            className="field w-full"
            type="password"
            autoComplete="new-password"
            value={again}
            aria-invalid={mismatch}
            onChange={(e) => setAgain(e.target.value)}
          />
        </label>
        {mismatch ? <p className="text-sm text-danger">{t('changePassword.mismatch')}</p> : null}
        {error ? (
          <p className="text-sm text-danger" role="alert">
            {error}
          </p>
        ) : null}
        <button type="submit" className="btn-primary" disabled={busy || !current || !next || next !== again}>
          {t('changePassword.submit')}
        </button>
        <button
          type="button"
          className="text-center text-xs text-ink-muted hover:text-ink"
          onClick={() => void api.signOut().finally(() => window.location.reload())}
        >
          {t('changePassword.signOut')}
        </button>
      </div>
    </form>
  )
}
