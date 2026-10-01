import { useEffect, useRef, useState } from 'react'
import type { AIProfile, Automation, AutomationInput, AutomationSchedule, Model, ToolRecord } from '@/types/api'
import { api } from '@/lib/api'
import { canChat } from '@/features/models/modelPresentation'
import type { AutomationPreview } from '@/types/api'
import { useUIStore } from '@/stores/uiStore'
import {
  civilInputValue,
  civilToISO,
  composePrompt,
  localTimeZone,
  notificationLabel,
  parseAutomationRequest,
  resultProse,
  scheduleLabel,
  visibleTask,
} from './parseRequest'

const WEEKDAYS = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday']

const NOTIFY_CHOICES: { mode: AutomationInput['notification']['mode']; label: string }[] = [
  { mode: 'condition', label: 'When the condition is true' },
  { mode: 'change', label: 'When the result changes' },
  { mode: 'always', label: 'Every time it runs' },
  { mode: 'failure', label: 'Only when it fails' },
  { mode: 'none', label: "Don't notify me" },
]

interface AutomationFormProps {
  profiles: AIProfile[]
  models: Model[]
  tools: ToolRecord[]
  initial?: Automation | null
  seedDescription?: string
  pending: boolean
  error?: string
  onCancel: () => void
  onSubmit: (input: AutomationInput) => void
}

export function AutomationForm({ profiles, models, tools, initial, seedDescription = '', pending, error, onCancel, onSubmit }: AutomationFormProps) {
  const advanced = useUIStore((state) => state.advancedMode) && new URLSearchParams(window.location.search).get('simple') !== '1'
  const advancedRef = useRef<HTMLDetailsElement>(null)
  const zone = initial?.schedule.time_zone || localTimeZone()
  const [description, setDescription] = useState('')
  const [notes, setNotes] = useState<string[]>([])
  const [parseError, setParseError] = useState('')
  const [name, setName] = useState(initial?.name ?? '')
  const [task, setTask] = useState(visibleTask(initial?.prompt ?? ''))
  const [preview, setPreview] = useState<AutomationPreview | null>(null)
  const [previewError, setPreviewError] = useState('')
  const [testing, setTesting] = useState(false)
  const installed = models.filter((model) => model.installed && canChat(model))
  const [profileID, setProfileID] = useState(initial?.profile_id || profiles.find((p) => p.id === 'general-assistant')?.id || profiles[0]?.id || '')
  const [modelID, setModelID] = useState(initial?.model_id || installed[0]?.id || '')
  const [schedule, setSchedule] = useState<AutomationSchedule>(initial?.schedule ?? { kind: 'daily', time_zone: zone, hour: 8, minute: 0 })
  const [mode, setMode] = useState(initial?.notification.mode ?? 'always')
  const [conditionKind, setConditionKind] = useState(initial?.notification.condition?.kind ?? 'threshold')
  const [op, setOp] = useState(initial?.notification.condition?.op ?? 'below')
  const [value, setValue] = useState(String(initial?.notification.condition?.value ?? ''))
  const [selectedTools, setSelectedTools] = useState<string[]>(initial?.tools ?? [])

  useEffect(() => {
    if (!seedDescription) return
    setDescription(seedDescription)
    try {
      const parsed = parseAutomationRequest(seedDescription, new Date(), zone)
      setName(parsed.name)
      setTask(visibleTask(parsed.prompt))
      setSchedule(parsed.schedule)
      setMode(parsed.notification.mode)
      setConditionKind(parsed.notification.condition?.kind ?? 'threshold')
      setOp(parsed.notification.condition?.op ?? 'below')
      setValue(parsed.notification.condition?.value == null ? '' : String(parsed.notification.condition.value))
      setNotes(parsed.notes)
      setParseError('')
    } catch (err) {
      setParseError(err instanceof Error ? err.message : 'That description could not be read.')
    }
  }, [seedDescription, zone])

  useEffect(() => {
    if (profileID && profiles.some((profile) => profile.id === profileID)) return
    const next = profiles.find((profile) => profile.id === 'general-assistant')?.id || profiles[0]?.id
    if (next) setProfileID(next)
  }, [profiles, profileID])

  useEffect(() => {
    if (modelID) return
    const first = models.find((model) => model.installed && canChat(model))
    if (first) setModelID(first.id)
  }, [models, modelID])

  useEffect(() => {
    if (advancedRef.current) advancedRef.current.open = advanced
  }, [advanced])

  // Tools a scheduled run may use are approved here, because nobody is
  // watching to answer later (spec §59). Tools that change things are listed
  // apart, so approving one is a deliberate choice.
  const lookTools = tools.filter((tool) => tool.enabled && tool.risk !== 'write')
  const changeTools = tools.filter((tool) => tool.enabled && tool.risk === 'write')

  function applyDescription() {
    try {
      const parsed = parseAutomationRequest(description, new Date(), schedule.time_zone || zone)
      setName(parsed.name)
      setTask(visibleTask(parsed.prompt))
      setSchedule(parsed.schedule)
      setMode(parsed.notification.mode)
      setConditionKind(parsed.notification.condition?.kind ?? 'threshold')
      setOp(parsed.notification.condition?.op ?? 'below')
      setValue(parsed.notification.condition?.value == null ? '' : String(parsed.notification.condition.value))
      setNotes(parsed.notes)
      setParseError('')
    } catch (err) {
      setParseError(err instanceof Error ? err.message : 'That description could not be read.')
    }
  }

  function draft(notification = currentNotification()): AutomationInput {
    return {
      name: name.trim() || 'Automation',
      prompt: composePrompt(task, notification),
      profile_id: profileID,
      model_id: modelID,
      schedule,
      notification,
      tools: selectedTools,
    }
  }

  function currentNotification(): AutomationInput['notification'] {
    if (mode !== 'condition') return { mode }
    if (conditionKind === 'threshold') {
      return { mode, condition: { kind: 'threshold', op, value: Number(value) } }
    }
    return { mode, condition: { kind: conditionKind } }
  }

  function submit() {
    if (!modelID || !task.trim()) return
    const notification = currentNotification()
    onSubmit({ ...draft(notification), name: name.trim() })
  }

  async function testDraft() {
    if (!modelID || !task.trim()) return
    setTesting(true)
    setPreview(null)
    setPreviewError('')
    try {
      setPreview(await api.previewAutomation(draft()))
    } catch (err) {
      setPreviewError(err instanceof Error ? err.message : 'The test could not run.')
    } finally {
      setTesting(false)
    }
  }

  const summary = scheduleLabel(schedule)

  return (
    <form
      className="card space-y-4"
      onSubmit={(event) => {
        event.preventDefault()
        submit()
      }}
    >
      <div>
        <h2 className="font-display text-lg font-semibold text-ink">{initial ? 'Edit automation' : 'New automation'}</h2>
        <p className="mt-1 text-sm text-ink-muted">Tell Yggdrasil what you want it to do.</p>
      </div>
      <label className="block space-y-1 text-sm">
        <span className="text-ink-muted">Describe what you want</span>
        <textarea
          className="field min-h-24 w-full"
          value={description}
          placeholder="Every morning at 8:00 AM, check this product and tell me if the price is below $500."
          onChange={(event) => setDescription(event.target.value)}
        />
      </label>
      <button type="button" className="btn-secondary px-3 py-1.5 text-xs" onClick={applyDescription}>
        Set up automation
      </button>
      {parseError && <p className="text-sm text-danger">{parseError}</p>}
      {notes.map((note) => (
        <p key={note} className="text-sm text-ink-muted">
          {note}
        </p>
      ))}
      <label className="block space-y-1 text-sm">
        <span className="text-ink-muted">Name</span>
        <input className="field w-full" value={name} onChange={(event) => setName(event.target.value)} required />
      </label>
      <label className="block space-y-1 text-sm">
        <span className="text-ink-muted">Task</span>
        <textarea className="field min-h-28 w-full" value={task} onChange={(event) => setTask(event.target.value)} required />
      </label>
      <div>
        <p className="text-sm text-ink-muted">Schedule</p>
        <p className="mt-1 text-sm text-ink">{summary}</p>
      </div>
      <div className="grid gap-3 sm:grid-cols-2">
        <label className="block space-y-1 text-sm">
          <span className="text-ink-muted">Repeats</span>
          <select
            className="field w-full"
            value={schedule.kind}
            onChange={(event) => setSchedule(changeKind(schedule, event.target.value as AutomationSchedule['kind']))}
          >
            <option value="daily">Every day</option>
            <option value="weekly">Every week</option>
            <option value="interval">On an interval</option>
            <option value="once">Once</option>
          </select>
        </label>
        <ScheduleFields schedule={schedule} onChange={setSchedule} />
      </div>
      <fieldset className="space-y-2">
        <legend className="text-sm text-ink-muted">Notify me</legend>
        {NOTIFY_CHOICES.map((choice) => (
          <label key={choice.mode} className="flex items-center gap-2 text-sm text-ink">
            <input
              type="radio"
              name="notify"
              checked={mode === choice.mode}
              onChange={() => setMode(choice.mode)}
            />
            {choice.label}
          </label>
        ))}
        <p className="text-xs text-ink-faint">Notices go to the bell in Yggdrasil and, if Settings allow, to this computer's notifications.</p>
      </fieldset>
      {mode === 'condition' && (
        <div className="grid gap-3 sm:grid-cols-2">
          <label className="block space-y-1 text-sm">
            <span className="text-ink-muted">Condition</span>
            <select
              className="field w-full"
              value={conditionKind}
              onChange={(event) => setConditionKind(event.target.value as 'threshold' | 'available' | 'significant')}
            >
              <option value="threshold">Price</option>
              <option value="available">Becomes available</option>
              <option value="significant">Significant result</option>
            </select>
          </label>
          {conditionKind === 'threshold' && (
            <>
              <label className="block space-y-1 text-sm">
                <span className="text-ink-muted">Price is</span>
                <select className="field w-full" value={op} onChange={(event) => setOp(event.target.value as 'below' | 'above')}>
                  <option value="below">below</option>
                  <option value="above">above</option>
                </select>
              </label>
              <label className="block space-y-1 text-sm sm:col-span-2">
                <span className="text-ink-muted">Amount</span>
                <input className="field w-full" inputMode="decimal" value={value} onChange={(event) => setValue(event.target.value)} required />
              </label>
            </>
          )}
        </div>
      )}
      <p className="text-sm text-ink">{notificationLabel(previewNotification(mode, conditionKind, op, value))}</p>
      {installed.length === 0 && <p className="text-sm text-danger">Install a model before creating an automation.</p>}
      <details ref={advancedRef} className="space-y-3">
        <summary className="cursor-pointer text-sm text-ink-muted">Advanced</summary>
        <label className="block space-y-1 text-sm">
          <span className="text-ink-muted">Profile</span>
          <select className="field w-full" value={profileID} onChange={(event) => setProfileID(event.target.value)}>
            {profiles.length === 0 && <option value="">No profiles yet</option>}
            {profiles.map((profile) => (
              <option key={profile.id} value={profile.id}>
                {profile.name}
              </option>
            ))}
          </select>
        </label>
        <label className="block space-y-1 text-sm">
          <span className="text-ink-muted">Model</span>
          <select className="field w-full" value={modelID} onChange={(event) => setModelID(event.target.value)} required>
            {installed.length === 0 && <option value="">Install a model first</option>}
            {installed.length > 0 && <option value="auto">Auto (Yggdrasil picks for each run)</option>}
            {installed.map((model) => (
              <option key={model.id} value={model.id}>
                {model.display_name || model.id}
              </option>
            ))}
          </select>
        </label>
        <label className="block space-y-1 text-sm">
          <span className="text-ink-muted">Time zone</span>
          <input
            className="field w-full"
            value={schedule.time_zone}
            onChange={(event) => setSchedule({ ...schedule, time_zone: event.target.value })}
            required
          />
        </label>
        {mode === 'condition' && (
          <p className="text-xs text-ink-faint">
            Yggdrasil checks the result against this condition. The task itself stays in ordinary language.
          </p>
        )}
        <fieldset className="space-y-2">
          <legend className="text-sm text-ink-muted">Tools it may use while you're away</legend>
          <p className="text-xs text-ink-faint">
            Approve what this automation needs now; nobody will be around to answer later. If it reaches a tool you
            didn't approve, it skips it and you get a notification. Leave everything unchecked to use only the read-only
            tools the profile allows.
          </p>
          <div className="grid gap-2 sm:grid-cols-2">
            {lookTools.map((tool) => (
              <label key={tool.id} className="flex items-start gap-2 text-sm text-ink">
                <input
                  type="checkbox"
                  className="mt-1"
                  checked={selectedTools.includes(tool.id)}
                  onChange={(event) => {
                    setSelectedTools((current) =>
                      event.target.checked ? [...current, tool.id] : current.filter((id) => id !== tool.id),
                    )
                  }}
                />
                <span>
                  {tool.name}
                  <span className="block text-xs text-ink-faint">{tool.description}</span>
                </span>
              </label>
            ))}
          </div>
          {changeTools.length > 0 && (
            <div className="rounded-lg border border-warning/30 bg-warning/5 p-3">
              <p className="text-xs font-medium text-ink">These change things on this computer</p>
              <p className="mb-2 text-xs text-ink-faint">Approve one only if this automation needs it. It will run without asking.</p>
            <div className="grid gap-2 sm:grid-cols-2">
              {changeTools.map((tool) => (
                <label key={tool.id} className="flex items-start gap-2 text-sm text-ink">
                  <input
                    type="checkbox"
                    className="mt-1"
                    checked={selectedTools.includes(tool.id)}
                    onChange={(event) => {
                      setSelectedTools((current) =>
                        event.target.checked ? [...current, tool.id] : current.filter((id) => id !== tool.id),
                      )
                    }}
                  />
                  <span>
                    {tool.name}
                    <span className="block text-xs text-ink-faint">{tool.description}</span>
                  </span>
                </label>
              ))}
            </div>
            </div>
          )}
        </fieldset>
      </details>
      {previewError && <p className="text-sm text-danger">{previewError}</p>}
      {preview && (
        <div className="rounded-lg bg-raised/50 p-3 text-sm">
          <p className="font-medium text-ink">{testing ? 'Testing automation…' : preview.error ? 'The test did not finish' : preview.would_notify ? 'A notice would be sent' : 'A notice would not be sent'}</p>
          {resultProse(preview.result) && <p className="mt-2 whitespace-pre-wrap text-ink-muted">{resultProse(preview.result)}</p>}
          {preview.error && <p className="mt-2 text-danger">{preview.error}</p>}
        </div>
      )}
      {testing && !preview && <p className="text-sm text-ink-muted">Testing automation…</p>}
      {error && <p className="text-sm text-danger">{error}</p>}
      <div className="flex flex-wrap gap-2">
        <button type="button" className="btn-secondary px-3 py-1.5 text-xs" disabled={testing || pending || !modelID || !task.trim()} onClick={() => void testDraft()}>
          {testing ? 'Testing…' : 'Test run'}
        </button>
        <button type="submit" className="btn-primary px-3 py-1.5 text-xs" disabled={pending || testing || profiles.length === 0 || !modelID || !task.trim()}>
          {pending ? 'Saving…' : initial ? 'Save changes' : 'Create automation'}
        </button>
        <button type="button" className="btn-secondary px-3 py-1.5 text-xs" onClick={onCancel}>
          Cancel
        </button>
      </div>
    </form>
  )
}

function ScheduleFields({
  schedule,
  onChange,
}: {
  schedule: AutomationSchedule
  onChange: (schedule: AutomationSchedule) => void
}) {
  if (schedule.kind === 'interval') {
    const { amount, unit } = splitInterval(schedule.every_seconds ?? 6 * 3600)
    return (
      <div className="grid gap-3 sm:grid-cols-2">
        <label className="block space-y-1 text-sm">
          <span className="text-ink-muted">Every</span>
          <input
            className="field w-full"
            type="number"
            min={1}
            value={amount}
            onChange={(event) => onChange({ ...schedule, every_seconds: Math.max(1, Number(event.target.value) || 1) * unitSeconds(unit) })}
          />
        </label>
        <label className="block space-y-1 text-sm">
          <span className="text-ink-muted">Unit</span>
          <select
            className="field w-full"
            value={unit}
            onChange={(event) => {
              const next = event.target.value as IntervalUnit
              onChange({ ...schedule, every_seconds: amount * unitSeconds(next) })
            }}
          >
            <option value="minutes">minutes</option>
            <option value="hours">hours</option>
            <option value="days">days</option>
          </select>
        </label>
      </div>
    )
  }
  if (schedule.kind === 'once') {
    return (
      <label className="block space-y-1 text-sm">
        <span className="text-ink-muted">When</span>
        <input
          className="field w-full"
          type="datetime-local"
          value={civilInputValue(schedule.at, schedule.time_zone)}
          onChange={(event) => onChange({ ...schedule, at: event.target.value ? civilToISO(event.target.value, schedule.time_zone) : undefined })}
          required
        />
      </label>
    )
  }
  return (
    <div className="grid gap-3 sm:grid-cols-2">
      <label className="block space-y-1 text-sm">
        <span className="text-ink-muted">Time</span>
        <input
          className="field w-full"
          type="time"
          value={`${String(schedule.hour ?? 0).padStart(2, '0')}:${String(schedule.minute ?? 0).padStart(2, '0')}`}
          onChange={(event) => {
            const [hour, minute] = event.target.value.split(':').map(Number)
            onChange({ ...schedule, hour, minute })
          }}
          required
        />
      </label>
      {schedule.kind === 'weekly' && (
        <label className="block space-y-1 text-sm">
          <span className="text-ink-muted">Day</span>
          <select
            className="field w-full"
            value={schedule.weekday ?? 1}
            onChange={(event) => onChange({ ...schedule, weekday: Number(event.target.value) })}
          >
            {WEEKDAYS.map((day, index) => (
              <option key={day} value={index}>
                {day}
              </option>
            ))}
          </select>
        </label>
      )}
    </div>
  )
}

type IntervalUnit = 'minutes' | 'hours' | 'days'

function unitSeconds(unit: IntervalUnit): number {
  if (unit === 'days') return 86400
  if (unit === 'hours') return 3600
  return 60
}

function splitInterval(seconds: number): { amount: number; unit: IntervalUnit } {
  if (seconds % 86400 === 0 && seconds >= 86400) return { amount: seconds / 86400, unit: 'days' }
  if (seconds % 3600 === 0 && seconds >= 3600) return { amount: seconds / 3600, unit: 'hours' }
  return { amount: Math.max(1, Math.round(seconds / 60)), unit: 'minutes' }
}

function changeKind(schedule: AutomationSchedule, kind: AutomationSchedule['kind']): AutomationSchedule {
  if (kind === 'once') return { kind, time_zone: schedule.time_zone, at: schedule.at }
  if (kind === 'interval') return { kind, time_zone: schedule.time_zone, every_seconds: schedule.every_seconds || 6 * 3600 }
  if (kind === 'weekly') {
    return { kind, time_zone: schedule.time_zone, hour: schedule.hour ?? 8, minute: schedule.minute ?? 0, weekday: schedule.weekday ?? 1 }
  }
  return { kind: 'daily', time_zone: schedule.time_zone, hour: schedule.hour ?? 8, minute: schedule.minute ?? 0 }
}

function previewNotification(mode: AutomationInput['notification']['mode'], kind: string, op: string, value: string): AutomationInput['notification'] {
  if (mode !== 'condition') return { mode }
  if (kind === 'threshold') {
    return { mode, condition: { kind: 'threshold', op: op === 'above' ? 'above' : 'below', value: Number(value) || 0 } }
  }
  if (kind === 'available') return { mode, condition: { kind: 'available' } }
  return { mode, condition: { kind: 'significant' } }
}
