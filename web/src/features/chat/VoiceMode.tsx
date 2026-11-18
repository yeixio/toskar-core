import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useQueryClient } from '@tanstack/react-query'
import { useDialog } from '@/lib/useDialog'
import { errorText } from '@/lib/api'
import { VoiceAudio, canUseMicrophone } from '@/lib/voice/audio'
import { VoiceConnection, type VoiceStatus } from '@/lib/voice/realtime'
import { readScreenshotLaunch } from '@/lib/screenshotMode'

/** Starts live voice mode: a conversation spoken aloud, both ways (#795). */
export function VoiceModeButton({ onStart }: { onStart: () => void }) {
  const { t } = useTranslation('chat')
  return (
    <button type="button" className="composer-attach" aria-label={t('voiceMode.start')} title={t('voiceMode.startHint')} onClick={onStart}>
      <svg viewBox="0 0 16 16" className="h-4 w-4" fill="none" stroke="currentColor" strokeWidth={1.5} strokeLinecap="round" aria-hidden>
        <path d="M2 7v2M5 4.5v7M8 2.5v11M11 4.5v7M14 7v2" />
      </svg>
    </button>
  )
}

/**
 * Live voice mode: the person talks, Toskar answers out loud, and they can
 * talk over the answer to stop it. Everything runs on their own computers;
 * the conversation is kept in their chats as Voice chat.
 */
export function VoiceMode({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation('chat')
  const queryClient = useQueryClient()
  const [status, setStatus] = useState<VoiceStatus>('connecting')
  const [you, setYou] = useState('')
  const [answer, setAnswer] = useState('')
  const [muted, setMuted] = useState(false)
  const [waited, setWaited] = useState<number | null>(null)
  const [error, setError] = useState('')
  const audioRef = useRef<VoiceAudio | null>(null)
  const ref = useDialog(true, onClose)

  useEffect(() => {
    // The user guide's screenshot shows a turn in progress, without a microphone.
    // The address loses screenshot=1 once the chat opens; the page keeps a mark.
    if (readScreenshotLaunch()?.enabled || document.documentElement.dataset.screenshot === '1') {
      setStatus('speaking')
      setYou(t('voiceMode.demo.you'))
      setAnswer(t('voiceMode.demo.answer'))
      setWaited(1100)
      return
    }
    if (!canUseMicrophone()) {
      setStatus('failed')
      setError(t('voiceMode.noMicrophone'))
      return
    }
    const audio = new VoiceAudio()
    audioRef.current = audio
    const conn = new VoiceConnection({
      onStatus: setStatus,
      onYou: setYou,
      onAnswer: setAnswer,
      onAudio: (samples) => audio.play(samples),
      onInterrupt: () => audio.interrupt(),
      onDone: (_, ms) => {
        if (ms != null) setWaited(ms)
        if (!audio.isPlaying) conn.played()
      },
      onError: (code, message) => {
        setError(code === 'VOICE_CONNECT_FAILED' ? t('voiceMode.connectFailed') : errorText(code, message || t('voiceMode.failed')))
      },
    })
    audio.onDrained = () => conn.played()
    audio.start((samples) => conn.send(samples)).catch((err: unknown) => {
      const denied = err instanceof DOMException && (err.name === 'NotAllowedError' || err.name === 'SecurityError')
      setError(denied ? t('voiceMode.micDenied') : t('voiceMode.micFailed'))
      conn.close()
      setStatus('failed')
    })
    return () => {
      conn.close()
      audio.close()
      audioRef.current = null
      // The conversation is one of their chats now.
      void queryClient.invalidateQueries({ queryKey: ['conversations'] })
    }
  }, [t, queryClient])

  const live = status !== 'closed' && status !== 'failed'
  const label = live ? t(`voiceMode.status.${status}`) : status === 'closed' && !error ? t('voiceMode.status.closed') : ''

  return (
    <div className="fixed inset-0 z-50 flex items-end justify-center scrim p-4 sm:items-center" role="presentation">
      <div
        ref={ref}
        className="card flex w-full max-w-md flex-col items-center gap-5 py-8 text-center shadow-panel"
        role="dialog"
        aria-modal="true"
        aria-labelledby="voice-mode-title"
      >
        <h2 id="voice-mode-title" className="sr-only">
          {t('voiceMode.title')}
        </h2>
        <div
          aria-hidden
          className={[
            'h-28 w-28 rounded-full transition-all duration-300',
            status === 'speaking'
              ? 'scale-110 bg-primary motion-safe:animate-pulse'
              : status === 'hearing'
                ? 'scale-105 bg-primary/70'
                : status === 'thinking'
                  ? 'bg-primary/40 motion-safe:animate-pulse'
                  : live
                    ? 'bg-primary/25'
                    : 'bg-line',
            muted ? 'opacity-40' : '',
          ].join(' ')}
        />
        <p className="min-h-5 text-sm font-medium text-ink" role="status" aria-live="polite">
          {muted && live ? t('voiceMode.muted') : label}
        </p>
        {you ? (
          <p className="max-w-full break-words text-sm text-ink-muted">
            <span className="font-medium text-ink">{t('voiceMode.you')}</span> {you}
          </p>
        ) : null}
        {answer ? <p className="max-h-40 max-w-full overflow-y-auto break-words text-sm text-ink">{answer}</p> : null}
        {error ? <p className="text-sm text-danger">{error}</p> : null}
        {waited != null ? <p className="text-xs text-ink-muted">{t('voiceMode.waited', { seconds: (waited / 1000).toFixed(1) })}</p> : null}
        <div className="flex gap-3">
          {live ? (
            <button
              type="button"
              className="btn-secondary"
              aria-pressed={muted}
              onClick={() => {
                audioRef.current?.setMuted(!muted)
                setMuted(!muted)
              }}
            >
              {muted ? t('voiceMode.unmute') : t('voiceMode.mute')}
            </button>
          ) : null}
          <button type="button" className="btn-primary" data-autofocus onClick={onClose}>
            {t('voiceMode.end')}
          </button>
        </div>
        <p className="text-xs text-ink-muted">{t('voiceMode.hint')}</p>
      </div>
    </div>
  )
}
