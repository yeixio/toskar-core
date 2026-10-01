import { ApiError } from '@/lib/api'
import type {
  ExampleFlag,
  MaterialUse,
  NodeTrainingFit,
  SpecializedAIView,
  TrainingJob,
  TrainingPreset,
  TrainingState,
} from '@/types/api'

export const TRAINING_EXPLAINER =
  'Training teaches your AI how to do its job. Connected knowledge gives it the current information it needs to do that job.'

export const useLabels: Record<MaterialUse, string> = {
  training: 'Training',
  knowledge: 'Knowledge',
  both: 'Both',
}

export const useDescriptions: Record<MaterialUse, string> = {
  training: 'Teaches how to respond. Baked into the model.',
  knowledge: 'Looked up when needed. Stays current when you edit it.',
  both: 'Examples teach the pattern; the facts stay connected.',
}

export const presetInfo: Record<TrainingPreset, { label: string; description: string }> = {
  quick: { label: 'Quick', description: 'A short run to check the idea. Good for tone and format.' },
  balanced: { label: 'Balanced', description: 'The default. Learns workflows and terminology from your examples.' },
  quality: { label: 'Highest quality', description: 'Longer training on every layer. Needs more examples, memory, and time.' },
}

export const flagLabels: Record<ExampleFlag, { label: string; blocking: boolean; help: string }> = {
  empty: { label: 'Empty', blocking: true, help: 'Has no question or answer.' },
  no_answer: { label: 'No answer', blocking: true, help: 'The last turn is not an answer, so there is nothing to learn from.' },
  duplicate: { label: 'Duplicate', blocking: true, help: 'Same as an earlier example. Only the first copy trains.' },
  too_long: { label: 'Too long', blocking: true, help: 'Longer than the model can train on. Shorten it or split it.' },
  short_answer: { label: 'Short answer', blocking: false, help: 'A one-word answer teaches little. It still trains.' },
  volatile_facts: {
    label: 'Changing facts',
    blocking: false,
    help: 'States a price, stock level, or SKU. The model may memorize a value that later changes. Keep those facts in knowledge.',
  },
}

const stateLabels: Record<TrainingState, string> = {
  queued: 'Queued',
  preparing_dataset: 'Preparing',
  loading_model: 'Loading model',
  training: 'Training',
  exporting: 'Exporting',
  evaluating: 'Testing',
  complete: 'Complete',
  failed: 'Failed',
  cancelled: 'Cancelled',
}

export function stateLabel(state: TrainingState): string {
  return stateLabels[state] ?? state
}

/** The steps a job passes through, in order, for the progress timeline. */
export const jobStages: TrainingState[] = [
  'queued',
  'preparing_dataset',
  'loading_model',
  'training',
  'exporting',
  'evaluating',
  'complete',
]

export function isTerminal(state: TrainingState): boolean {
  return state === 'complete' || state === 'failed' || state === 'cancelled'
}

export function formatDuration(totalSec: number | undefined | null): string {
  if (totalSec == null || totalSec < 0) return '—'
  const sec = Math.round(totalSec)
  if (sec < 60) return `${sec}s`
  const m = Math.floor(sec / 60)
  const s = sec % 60
  if (m < 60) return s ? `${m}m ${s}s` : `${m}m`
  const h = Math.floor(m / 60)
  const rm = m % 60
  return rm ? `${h}h ${rm}m` : `${h}h`
}

export function elapsedSec(job: TrainingJob, now: number = Date.now()): number {
  if (!job.started_at) return 0
  const start = Date.parse(job.started_at)
  const end = job.finished_at ? Date.parse(job.finished_at) : now
  return Math.max(0, (end - start) / 1000)
}

export function jobPercent(job: TrainingJob): number {
  const p = job.progress
  if (job.state === 'complete') return 100
  if (p.iter && p.iters) return Math.min(99, Math.round((p.iter / p.iters) * 100))
  return 0
}

export function fitTone(fit: Pick<NodeTrainingFit, 'label'>): string {
  switch (fit.label) {
    case 'comfortable':
      return 'bg-success/15 text-success'
    case 'tight':
      return 'bg-warning/15 text-warning'
    default:
      return 'bg-danger/15 text-danger'
  }
}

export const fitLabels: Record<NodeTrainingFit['label'], string> = {
  comfortable: 'Fits',
  tight: 'Tight fit',
  too_large: 'Too large',
  unsupported: 'Not supported',
}

export type StepID = 'describe' | 'base' | 'material' | 'examples' | 'plan' | 'train' | 'test' | 'deploy'

export const steps: { id: StepID; label: string }[] = [
  { id: 'describe', label: 'Describe' },
  { id: 'base', label: 'Base model' },
  { id: 'material', label: 'Material' },
  { id: 'examples', label: 'Examples' },
  { id: 'plan', label: 'Review' },
  { id: 'train', label: 'Train' },
  { id: 'test', label: 'Test' },
  { id: 'deploy', label: 'Deploy' },
]

/** Which steps are done, so the step bar can show progress through the build. */
export function completedSteps(view: SpecializedAIView): Set<StepID> {
  const done = new Set<StepID>()
  if (view.name && view.instructions) done.add('describe')
  if (view.base_model_id) done.add('base')
  if (view.materials.length > 0) done.add('material')
  if (view.dataset.usable >= 10) done.add('examples')
  if (view.jobs.length > 0) done.add('plan')
  if (view.revisions.length > 0) done.add('train')
  if (view.deployable_revisions.length > 0) done.add('test')
  if (view.deployed_revision > 0) done.add('deploy')
  return done
}

/** The first step that still needs attention. */
export function nextStep(view: SpecializedAIView): StepID {
  const done = completedSteps(view)
  const active = view.jobs.find((j) => !isTerminal(j.state))
  if (active) return 'train'
  for (const s of steps) {
    if (!done.has(s.id)) return s.id
  }
  return 'deploy'
}

export function lastUserTurn(messages: { role: string; content: string }[]): string {
  for (let i = messages.length - 1; i >= 0; i -= 1) {
    if (messages[i].role === 'user') return messages[i].content
  }
  return ''
}

export function lastAnswer(messages: { role: string; content: string }[]): string {
  for (let i = messages.length - 1; i >= 0; i -= 1) {
    if (messages[i].role === 'assistant') return messages[i].content
  }
  return ''
}

export function errorText(error: unknown, fallback = 'Something went wrong.'): string {
  return error instanceof ApiError || error instanceof Error ? error.message : fallback
}

// exportRevision is the revision to export as a GGUF file: the deployed one,
// or else the newest.
export function exportRevision(view: Pick<SpecializedAIView, 'deployed_revision' | 'revisions'>): number {
  if (view.deployed_revision > 0) return view.deployed_revision
  return view.revisions.reduce((max, r) => Math.max(max, r.revision), 0)
}
