import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useDialog } from '@/lib/useDialog'
import { api, errorText } from '@/lib/api'
import {
  VoiceAudio,
  canUseMicrophone,
  listMicrophones,
  readMicrophoneChoice,
  saveMicrophoneChoice,
  type MicrophoneChoice,
} from '@/lib/voice/audio'
import { VoiceConnection, resolveVoiceSocketUrl, type VoiceStatus } from '@/lib/voice/realtime'
import { readScreenshotLaunch } from '@/lib/screenshotMode'
import { formatLocale } from '@/i18n/format'
import { VoiceAvatar } from './VoiceAvatar'
import { isAvatar } from './avatars'

/** A heard language's name, such as Spanish for es, in the App language. */
function heardName(tag: string): string {
  try {
    return new Intl.DisplayNames([formatLocale()], { type: 'language' }).of(tag) ?? tag
  } catch {
    return tag
  }
}

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
  // The language heard, so a wrong guess can be seen (#1405).
  const [heard, setHeard] = useState('')
  const [answer, setAnswer] = useState('')
  const [muted, setMuted] = useState(false)
  const [waited, setWaited] = useState<number | null>(null)
  const [error, setError] = useState('')
  // Which microphone, and whether the browser evens out its level (#1330).
  const [mic, setMic] = useState<MicrophoneChoice>(() => readMicrophoneChoice())
  const [mics, setMics] = useState<{ deviceId: string; label: string }[]>([])
  const audioRef = useRef<VoiceAudio | null>(null)
  const ref = useDialog(true, onClose)
  const settings = useQuery({ queryKey: ['settings'], queryFn: () => api.getSettings(), retry: false })
  const thinkingSound = settings.data?.thinking_sound ?? true
  // The character the person chose to see, if any (#1369).
  const avatar = settings.data?.voice_avatar

  // A soft chime while an answer takes a while, so the person knows they
  // were heard (#1334); it stops the moment anything else happens.
  useEffect(() => {
    const audio = audioRef.current
    if (!audio) return
    if (status === 'thinking' && thinkingSound) audio.startThinking()
    else audio.stopThinking()
  }, [status, thinkingSound])

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
    let conn: VoiceConnection | null = null
    let ended = false
    const handlers = {
      onStatus: setStatus,
      onYou: (text: string, language?: string) => {
        setYou(text)
        setHeard(language ?? '')
      },
      onAnswer: setAnswer,
      onAudio: (samples: Float32Array) => {
        audio.stopThinking()
        audio.play(samples)
      },
      onInterrupt: () => {
        audio.stopThinking()
        audio.interrupt()
      },
      onDone: (_: string, ms?: number) => {
        if (ms != null) setWaited(ms)
        if (!audio.isPlaying) conn?.played()
      },
      onError: (code: string, message: string) => {
        setError(code === 'VOICE_CONNECT_FAILED' ? t('voiceMode.connectFailed') : errorText(code, message || t('voiceMode.failed')))
      },
    }
    audio.onDrained = () => conn?.played()
    // The desktop app says where the daemon is; a page in a browser is on it.
    void resolveVoiceSocketUrl().then((url) => {
      if (!ended) conn = new VoiceConnection(handlers, undefined, url)
    })
    audio
      .start((samples) => conn?.send(samples), readMicrophoneChoice())
      .then(async () => {
        if (ended) return
        // Labels are known once the microphone is allowed.
        const list = await listMicrophones()
        const using = audio.microphone
        setMics(list.length ? list : using.deviceId ? [using] : [])
        setMic((m) => ({ ...m, deviceId: using.deviceId || m.deviceId }))
      })
      .catch((err: unknown) => {
        const denied = err instanceof DOMException && (err.name === 'NotAllowedError' || err.name === 'SecurityError')
        setError(denied ? t('voiceMode.micDenied') : t('voiceMode.micFailed'))
        ended = true
        conn?.close()
        setStatus('failed')
      })
    return () => {
      ended = true
      conn?.close()
      audio.close()
      audioRef.current = null
      // The conversation is one of their chats now.
      void queryClient.invalidateQueries({ queryKey: ['conversations'] })
    }
  }, [t, queryClient])

  const live = status !== 'closed' && status !== 'failed'

  const chooseMicrophone = (next: MicrophoneChoice) => {
    setMic(next)
    saveMicrophoneChoice(next)
    audioRef.current?.useMicrophone(next).catch(() => setError(t('voiceMode.micFailed')))
  }
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
        {isAvatar(avatar) ? (
          <VoiceAvatar character={avatar} status={status} level={() => audioRef.current?.level() ?? 0} muted={muted} />
        ) : (
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
        )}
        <p className="min-h-5 text-sm font-medium text-ink" role="status" aria-live="polite">
          {muted && live ? t('voiceMode.muted') : label}
        </p>
        {you ? (
          <p className="max-w-full break-words text-sm text-ink-muted">
            <span className="font-medium text-ink">{heard ? t('voiceMode.youIn', { language: heardName(heard) }) : t('voiceMode.you')}</span> {you}
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
        {live && mics.length > 0 ? (
          <div className="flex w-full flex-col items-center gap-2 text-sm">
            <label className="flex w-full max-w-xs flex-col gap-1 text-start">
              <span className="text-xs text-ink-muted">{t('voiceMode.microphone')}</span>
              <select className="input" value={mic.deviceId} onChange={(e) => chooseMicrophone({ ...mic, deviceId: e.target.value })}>
                {mics.map((m, i) => (
                  <option key={m.deviceId} value={m.deviceId}>
                    {m.label || t('voiceMode.microphoneN', { n: i + 1 })}
                  </option>
                ))}
              </select>
            </label>
            <label className="flex max-w-xs items-start gap-2 text-start">
              <input type="checkbox" className="mt-1" checked={mic.autoGain} onChange={(e) => chooseMicrophone({ ...mic, autoGain: e.target.checked })} />
              <span>
                {t('voiceMode.autoGain')}
                <span className="block text-xs text-ink-faint">{t('voiceMode.autoGainHint')}</span>
              </span>
            </label>
          </div>
        ) : null}
        <p className="text-xs text-ink-muted">{t('voiceMode.hint')}</p>
      </div>
    </div>
  )
}
