import { useEffect, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { api } from '@/lib/api'
import { ShownError, shownOr } from '@/components/ui/ShownError'
import type { VoiceChoice, VoicePersona, VoicePersonaInput, VoicePersonas as Personas } from '@/types/api'
import { voiceSample } from './voiceSamples'
import { byLanguageName, languageName } from './languageNames'

const queryKey = ['voice-personas']

/** A speed as people read it, such as 1.25×. */
function speedText(speed: number): string {
  return `${Number(speed.toFixed(2))}×`
}

/**
 * Voice personas (#1319): everyone picks the one they're read aloud in;
 * an Admin, given the languages and their voices, makes, changes, and
 * deletes them.
 */
export function VoicePersonas({ languages }: { languages?: VoiceChoice[] }) {
  const { t } = useTranslation('settings')
  const queryClient = useQueryClient()
  const personas = useQuery({ queryKey, queryFn: () => api.getVoicePersonas(), retry: false })
  const update = (data: Personas | null) => {
    if (data) queryClient.setQueryData(queryKey, data)
  }
  const pick = useMutation({ mutationFn: (id: string) => api.pickVoicePersona(id), onSuccess: update })
  const remove = useMutation({ mutationFn: (id: string) => api.deleteVoicePersona(id), onSuccess: update })
  const [editing, setEditing] = useState<VoicePersona | 'new' | null>(null)
  const [confirmDelete, setConfirmDelete] = useState('')

  const data = personas.data
  // An older computer has no personas: there's nothing to show.
  if (!data || !Array.isArray(data.personas)) return null
  const manage = Boolean(languages?.length)
  const voiceName = (language: string, id: string) => languages?.find((c) => c.language === language)?.voices.find((v) => v.id === id)?.name ?? id
  const error = pick.error ?? remove.error

  return (
    <div className="space-y-3" aria-labelledby="voice-personas-title">
      <div>
        <p id="voice-personas-title" className="text-sm font-medium text-ink">
          {t('voice.personas.title')}
        </p>
        <p className="mt-0.5 text-xs text-ink-muted">{t('voice.personas.description')}</p>
      </div>

      <div className="flex flex-wrap items-center gap-3">
        <label htmlFor="voice-persona-pick" className="w-32 shrink-0 text-sm text-ink">
          {t('voice.personas.yours')}
        </label>
        <select
          id="voice-persona-pick"
          className="field min-w-0 flex-1"
          value={data.picked}
          disabled={pick.isPending}
          onChange={(e) => pick.mutate(e.target.value)}
        >
          <option value="">{t('voice.personas.none')}</option>
          {data.personas.map((p) => (
            <option key={p.id} value={p.id}>
              {p.name}
            </option>
          ))}
        </select>
      </div>
      <p className="text-xs text-ink-muted">{t('voice.personas.yoursHint')}</p>
      {data.personas.length === 0 && !manage ? <p className="text-xs text-ink-muted">{t('voice.personas.memberEmpty')}</p> : null}

      {manage ? (
        <ul className="divide-y divide-line">
          {data.personas.map((p) =>
            editing !== 'new' && editing?.id === p.id ? (
              <li key={p.id} className="py-2">
                <PersonaEditor languages={languages!} persona={p} onDone={(d) => (update(d), setEditing(null))} onCancel={() => setEditing(null)} />
              </li>
            ) : (
              <li key={p.id} className="flex flex-wrap items-center gap-3 py-2">
                <div className="min-w-0 flex-1">
                  <p className="text-sm text-ink">{p.name}</p>
                  <p className="text-xs text-ink-muted">
                    {t('voice.personas.summary', { voice: voiceName(p.language, p.voice), language: languageName(p.language), speed: speedText(p.speed) })}
                  </p>
                  {!p.reads ? (
                    <p className="text-xs text-warning">{t('voice.personas.readsWith', { voice: voiceName(p.language, p.reads_with ?? '') })}</p>
                  ) : null}
                </div>
                {confirmDelete === p.id ? (
                  <div className="flex shrink-0 gap-2">
                    <button
                      type="button"
                      className="btn-danger px-3 py-1.5 text-sm"
                      disabled={remove.isPending}
                      onClick={() => {
                        setConfirmDelete('')
                        remove.mutate(p.id)
                      }}
                    >
                      {t('voice.personas.deleteConfirm', { name: p.name })}
                    </button>
                    <button type="button" className="btn-secondary px-3 py-1.5 text-sm" onClick={() => setConfirmDelete('')}>
                      {t('voice.personas.keep')}
                    </button>
                  </div>
                ) : (
                  <div className="flex shrink-0 gap-2">
                    <button type="button" className="btn-secondary px-3 py-1.5 text-sm" onClick={() => setEditing(p)}>
                      {t('voice.personas.edit')}
                    </button>
                    <button type="button" className="btn-ghost px-3 py-1.5 text-sm" onClick={() => setConfirmDelete(p.id)}>
                      {t('voice.personas.delete')}
                    </button>
                  </div>
                )}
              </li>
            ),
          )}
          <li className="py-2">
            {editing === 'new' ? (
              <PersonaEditor languages={languages!} onDone={(d) => (update(d), setEditing(null))} onCancel={() => setEditing(null)} />
            ) : (
              <button type="button" className="btn-secondary px-3 py-1.5 text-sm" onClick={() => setEditing('new')}>
                {t('voice.personas.add')}
              </button>
            )}
          </li>
        </ul>
      ) : null}
      {error ? <ShownError {...shownOr(error, t('voice.failed'))} role="alert" /> : null}
    </div>
  )
}

function PersonaEditor({
  languages,
  persona,
  onDone,
  onCancel,
}: {
  languages: VoiceChoice[]
  persona?: VoicePersona
  onDone: (data: Personas | null) => void
  onCancel: () => void
}) {
  const { t } = useTranslation('settings')
  const ordered = byLanguageName(languages.map((c) => c.language))
  const [draft, setDraft] = useState<VoicePersonaInput>(() => {
    if (persona) return { name: persona.name, language: persona.language, voice: persona.voice, speed: persona.speed }
    const language = ordered.includes('en') ? 'en' : ordered[0] ?? ''
    const choice = languages.find((c) => c.language === language)
    return { name: '', language, voice: choice?.voice || choice?.automatic || '', speed: 1 }
  })
  const choice = languages.find((c) => c.language === draft.language)
  const save = useMutation({
    mutationFn: () => (persona ? api.updateVoicePersona(persona.id, draft) : api.createVoicePersona(draft)),
    onSuccess: onDone,
  })
  const [playing, setPlaying] = useState(false)
  const [failed, setFailed] = useState(false)
  const audio = useRef<HTMLAudioElement | null>(null)
  useEffect(() => () => audio.current?.pause(), [])
  const id = persona?.id ?? 'new'

  const preview = async () => {
    setFailed(false)
    setPlaying(true)
    try {
      const blob = await api.previewVoice(voiceSample(draft.language), draft.voice, draft.language, draft.speed)
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
    <form
      className="space-y-2 rounded-lg border border-line p-3"
      onSubmit={(e) => {
        e.preventDefault()
        save.mutate()
      }}
    >
      <div className="flex flex-wrap items-center gap-3">
        <label htmlFor={`persona-name-${id}`} className="w-24 shrink-0 text-sm text-ink">
          {t('voice.personas.name')}
        </label>
        <input
          id={`persona-name-${id}`}
          className="field min-w-0 flex-1"
          maxLength={40}
          required
          value={draft.name}
          onChange={(e) => setDraft({ ...draft, name: e.target.value })}
        />
      </div>
      <div className="flex flex-wrap items-center gap-3">
        <label htmlFor={`persona-language-${id}`} className="w-24 shrink-0 text-sm text-ink">
          {t('voice.personas.language')}
        </label>
        <select
          id={`persona-language-${id}`}
          className="field min-w-0 flex-1"
          value={draft.language}
          onChange={(e) => {
            const next = languages.find((c) => c.language === e.target.value)
            setDraft({ ...draft, language: e.target.value, voice: next?.voice || next?.automatic || '' })
          }}
        >
          {ordered.map((l) => (
            <option key={l} value={l}>
              {languageName(l)}
            </option>
          ))}
        </select>
      </div>
      <div className="flex flex-wrap items-center gap-3">
        <label htmlFor={`persona-voice-${id}`} className="w-24 shrink-0 text-sm text-ink">
          {t('voice.personas.voice')}
        </label>
        <select
          id={`persona-voice-${id}`}
          className="field min-w-0 flex-1"
          value={draft.voice}
          onChange={(e) => setDraft({ ...draft, voice: e.target.value })}
        >
          {(choice?.voices ?? []).map((v) => (
            <option key={v.id} value={v.id}>
              {`${v.standard ? t('voice.standard') : v.name} (${t(`voice.engine.${v.engine}`)})`}
            </option>
          ))}
        </select>
      </div>
      <div className="flex flex-wrap items-center gap-3">
        <label htmlFor={`persona-speed-${id}`} className="w-24 shrink-0 text-sm text-ink">
          {t('voice.personas.speed')}
        </label>
        <input
          id={`persona-speed-${id}`}
          type="range"
          className="min-w-0 flex-1"
          min={0.5}
          max={2}
          step={0.05}
          value={draft.speed}
          onChange={(e) => setDraft({ ...draft, speed: Number(e.target.value) })}
        />
        <span className="w-12 text-right text-sm text-ink-muted">{speedText(draft.speed)}</span>
      </div>
      <div className="flex flex-wrap items-center gap-2">
        <button type="submit" className="btn-primary px-3 py-1.5 text-sm" disabled={save.isPending || !draft.name.trim() || !draft.voice}>
          {t('voice.personas.save')}
        </button>
        <button type="button" className="btn-secondary px-3 py-1.5 text-sm" disabled={playing || !draft.voice} onClick={() => void preview()}>
          {t('voice.preview')}
        </button>
        <button type="button" className="btn-ghost px-3 py-1.5 text-sm" onClick={onCancel}>
          {t('voice.personas.cancel')}
        </button>
        {failed ? <span className="text-xs text-danger">{t('voice.previewFailed')}</span> : null}
      </div>
      {save.error ? <ShownError {...shownOr(save.error, t('voice.failed'))} role="alert" /> : null}
    </form>
  )
}
