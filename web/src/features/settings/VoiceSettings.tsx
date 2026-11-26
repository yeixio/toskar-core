import { useEffect, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { api } from '@/lib/api'
import { ShownError, shownOr } from '@/components/ui/ShownError'
import { formatLocale } from '@/i18n/format'
import type { VoiceChoice, VoiceOption, VoiceSettings as Settings } from '@/types/api'
import { voiceSample } from './voiceSamples'
import { ModelNeeds } from './ModelNeeds'
import { answerLanguages } from '@/i18n/answerLanguages'

const queryKey = ['speech-voices']

export function languageName(tag: string): string {
  try {
    return new Intl.DisplayNames([formatLocale()], { type: 'language' }).of(tag) ?? tag
  } catch {
    return tag
  }
}

/** Language tags in the order of their names as shown, not their codes (#1342). */
export function byLanguageName(tags: string[]): string[] {
  const collator = new Intl.Collator(formatLocale())
  return [...tags].sort((a, b) => collator.compare(languageName(a), languageName(b)))
}

/** The answer languages nothing here reads aloud, such as Japanese (#1342). */
export function unvoicedLanguages(voiced: string[]): string[] {
  const has = new Set(voiced.map((l) => l.split('-')[0].toLowerCase()))
  const alias: Record<string, string> = { no: 'nb', nn: 'nb' }
  const missing = answerLanguages
    .map((tag) => tag.split('-')[0].toLowerCase())
    .map((base) => alias[base] ?? base)
    .filter((base, i, all) => all.indexOf(base) === i && !has.has(base))
  return byLanguageName(missing)
}

/**
 * The Voice settings (#1304), for Admins: install Kitten TTS 2 mini, and
 * choose the voice each language is read aloud in, for live voice mode,
 * read-aloud on the phone and in the car, and the doorbell.
 */
export function VoiceSettings() {
  const { t } = useTranslation('settings')
  const queryClient = useQueryClient()
  const voices = useQuery({
    queryKey,
    queryFn: () => api.getVoiceSettings(),
    retry: false,
    // While Kitten installs, check how it's going.
    refetchInterval: (q) => ((q.state.data as Settings | null | undefined)?.kitten?.state === 'installing' ? 2000 : false),
  })
  const update = (data: Settings | null) => {
    if (data) queryClient.setQueryData(queryKey, data)
  }
  const install = useMutation({ mutationFn: () => api.installKitten(), onSuccess: update })
  const remove = useMutation({ mutationFn: () => api.removeKitten(), onSuccess: update })
  const choose = useMutation({
    mutationFn: ({ language, voice }: { language: string; voice: string }) => api.setSpeechVoice(language, voice),
    onSuccess: update,
  })
  const [confirmRemove, setConfirmRemove] = useState(false)

  const data = voices.data
  // An older computer answers no Voice settings: there's nothing to show.
  if (!data?.kitten || !Array.isArray(data.languages)) {
    return voices.isError ? (
      <section className="card space-y-2">
        <h2 className="section-title">{t('voice.title')}</h2>
        <ShownError {...shownOr(voices.error, t('voice.unavailable'))} className="text-sm text-ink-muted" />
      </section>
    ) : null
  }
  const kitten = data.kitten
  const error = install.error ?? remove.error ?? choose.error

  return (
    <section className="card space-y-4" aria-labelledby="voice-settings-title">
      <div>
        <h2 id="voice-settings-title" className="section-title">{t('voice.title')}</h2>
        <p className="mt-1 text-sm text-ink-muted">{t('voice.description')}</p>
      </div>

      <div className="rounded-lg border border-line p-3 space-y-2">
        <div className="flex items-start justify-between gap-4">
          <div>
            <p className="text-sm font-medium text-ink">{t('voice.kitten.name')}</p>
            <p className="mt-0.5 text-xs text-ink-muted">
              {t('voice.kitten.description', { languages: byLanguageName(kitten.languages).map(languageName).join(', ') })}
            </p>
            <p className="mt-0.5 text-xs text-ink-faint">
              <a href={kitten.url} target="_blank" rel="noreferrer" className="underline">
                {kitten.credit}
              </a>
              {' · '}
              {kitten.license}
            </p>
          </div>
          {kitten.state === 'installed' ? (
            confirmRemove ? (
              <div className="flex shrink-0 gap-2">
                <button
                  type="button"
                  className="btn-danger px-3 py-1.5 text-sm"
                  disabled={remove.isPending}
                  onClick={() => {
                    setConfirmRemove(false)
                    remove.mutate()
                  }}
                >
                  {t('voice.kitten.removeConfirm')}
                </button>
                <button type="button" className="btn-secondary px-3 py-1.5 text-sm" onClick={() => setConfirmRemove(false)}>
                  {t('voice.kitten.keep')}
                </button>
              </div>
            ) : (
              <button type="button" className="btn-secondary shrink-0 px-3 py-1.5 text-sm" disabled={remove.isPending} onClick={() => setConfirmRemove(true)}>
                {t('voice.kitten.remove')}
              </button>
            )
          ) : kitten.state === 'installing' ? (
            <span className="shrink-0 text-sm text-ink-muted" role="status">
              {kitten.stage === 'voices' ? t('voice.kitten.downloading') : kitten.stage === 'warming' ? t('voice.kitten.warming') : kitten.stage === 'listening' ? t('voice.kitten.listening') : t('voice.kitten.installing')}
            </span>
          ) : (
            <button
              type="button"
              className="btn-primary shrink-0 px-3 py-1.5 text-sm"
              disabled={Boolean(kitten.unavailable) || install.isPending}
              onClick={() => install.mutate()}
            >
              {kitten.state === 'failed' ? t('voice.kitten.retry') : t('voice.kitten.install')}
            </button>
          )}
        </div>
        {kitten.state === 'failed' && kitten.error ? <p className="text-xs text-danger">{kitten.error}</p> : null}
        {kitten.unavailable ? <p className="text-xs text-ink-muted">{kitten.unavailable}</p> : null}
      </div>

      {error ? <ShownError {...shownOr(error, t('voice.failed'))} role="alert" /> : null}

      <ModelNeeds needs={data.needs ?? []} />

      <div className="space-y-2">
        <p className="text-sm font-medium text-ink">{t('voice.languages')}</p>
        <ul className="divide-y divide-line">
          {byLanguageName(data.languages.map((c) => c.language))
            .map((language) => data.languages.find((c) => c.language === language)!)
            .map((choice) => (
            <VoiceRow
              key={choice.language}
              choice={choice}
              busy={choose.isPending}
              onChoose={(voice) => choose.mutate({ language: choice.language, voice })}
            />
          ))}
        </ul>
        {unvoicedLanguages(data.languages.map((c) => c.language)).length > 0 ? (
          <p className="text-xs text-ink-muted">
            {t('voice.noVoice', { languages: unvoicedLanguages(data.languages.map((c) => c.language)).map(languageName).join(', ') })}
          </p>
        ) : null}
      </div>
    </section>
  )
}

function optionLabel(v: VoiceOption, engine: (e: VoiceOption['engine']) => string, standard: string): string {
  return `${v.standard ? standard : v.name} (${engine(v.engine)})${v.description ? ` · ${v.description}` : ''}`
}

function VoiceRow({ choice, busy, onChoose }: { choice: VoiceChoice; busy: boolean; onChoose: (voice: string) => void }) {
  const { t } = useTranslation('settings')
  const [playing, setPlaying] = useState(false)
  const [failed, setFailed] = useState(false)
  const audio = useRef<HTMLAudioElement | null>(null)
  useEffect(() => () => audio.current?.pause(), [])
  const engine = (e: VoiceOption['engine']) => t(`voice.engine.${e}`)
  const automatic = choice.voices.find((v) => v.id === choice.automatic)
  const name = languageName(choice.language)
  const selectId = `voice-${choice.language}`

  const preview = async () => {
    setFailed(false)
    setPlaying(true)
    try {
      const blob = await api.previewVoice(voiceSample(choice.language), choice.voice || choice.automatic, choice.language)
      const url = URL.createObjectURL(blob)
      audio.current?.pause()
      const a = new Audio(url)
      audio.current = a
      a.onended = () => {
        URL.revokeObjectURL(url)
        setPlaying(false)
      }
      await a.play()
    } catch {
      setFailed(true)
      setPlaying(false)
    }
  }

  return (
    <li className="flex flex-wrap items-center gap-3 py-2">
      <label htmlFor={selectId} className="w-32 shrink-0 text-sm text-ink">
        {name}
      </label>
      <select id={selectId} className="field min-w-0 flex-1" value={choice.voice} disabled={busy} onChange={(e) => onChoose(e.target.value)}>
        <option value="">{automatic ? t('voice.automaticNamed', { voice: optionLabel(automatic, engine, t('voice.standard')) }) : t('voice.automatic')}</option>
        {choice.voices.map((v) => (
          <option key={v.id} value={v.id}>
            {optionLabel(v, engine, t('voice.standard'))}
          </option>
        ))}
      </select>
      <button
        type="button"
        className="btn-secondary px-3 py-1.5 text-sm"
        disabled={playing}
        aria-label={t('voice.previewIn', { language: name })}
        onClick={() => void preview()}
      >
        {t('voice.preview')}
      </button>
      {failed ? <span className="text-xs text-danger">{t('voice.previewFailed')}</span> : null}
    </li>
  )
}
