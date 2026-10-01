import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { EmptyState } from '@/components/ui/EmptyState'
import { api } from '@/lib/api'
import { subscribeEvents } from '@/lib/events'
import type { SpecializedAI, SpecializedAIView } from '@/types/api'
import { ConceptCards } from './ConceptCards'
import { ExampleNote } from './ExampleNote'
import { BaseModelStep } from './steps/BaseModelStep'
import { DeployStep } from './steps/DeployStep'
import { DescribeStep } from './steps/DescribeStep'
import { ExamplesStep } from './steps/ExamplesStep'
import { MaterialStep } from './steps/MaterialStep'
import { PlanStep } from './steps/PlanStep'
import { TestStep } from './steps/TestStep'
import { TrainStep } from './steps/TrainStep'
import { completedSteps, errorText, isTerminal, nextStep, stateLabel, steps, type StepID } from './display'
import { RealmKicker } from '@/components/ui/Realm'

export function TrainPage() {
  const queryClient = useQueryClient()
  const [params, setParams] = useSearchParams()
  const selectedID = params.get('ai')
  const [creating, setCreating] = useState(false)

  const listQuery = useQuery({ queryKey: ['training', 'ais'], queryFn: () => api.listAIs() })
  const viewQuery = useQuery({
    queryKey: ['training', 'ai', selectedID],
    queryFn: () => api.getAI(selectedID ?? ''),
    enabled: Boolean(selectedID),
  })

  useEffect(() => {
    return subscribeEvents({
      onEvent: (event) => {
        if (!event.type.startsWith('training.')) return
        void queryClient.invalidateQueries({ queryKey: ['training'] })
      },
    })
  }, [queryClient])

  const items = listQuery.data ?? []
  const select = (id: string | null) => {
    setCreating(false)
    setParams(id ? { ai: id } : {}, { replace: false })
  }
  const example = useMutation({
    mutationFn: () => api.createExampleAI(),
    onSuccess: (ai) => {
      void queryClient.invalidateQueries({ queryKey: ['training'] })
      if (ai) select(ai.id)
    },
  })
  const exampleButton = (className: string) => (
    <button type="button" className={className} disabled={example.isPending} onClick={() => example.mutate()}>
      {example.isPending ? 'Setting up the example…' : 'Try an example'}
    </button>
  )

  return (
    <div className="page-fill gap-4 overflow-y-auto p-4">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <RealmKicker />
          <h1 className="font-display text-2xl font-semibold text-ink">Train your own AI</h1>
          <p className="mt-1 max-w-2xl text-sm text-ink-muted">
            Build a small assistant that is good at one job. Show it how to respond with examples, and connect the
            information it needs. You do not need to know how models are trained.
          </p>
        </div>
        <div className="flex gap-2">
          {exampleButton('btn-secondary px-3 py-1.5 text-xs')}
          <button type="button" className="btn-primary px-3 py-1.5 text-xs" onClick={() => { select(null); setCreating(true) }}>
            Build a new AI
          </button>
        </div>
      </div>
      {example.error && <p className="text-sm text-danger">{errorText(example.error)}</p>}

      <div className="grid gap-4 xl:grid-cols-[16rem_minmax(0,1fr)]">
        <aside className="space-y-2">
          {listQuery.isLoading && <p className="text-sm text-ink-muted">Loading…</p>}
          {items.map((ai) => (
            <AIListItem key={ai.id} ai={ai} active={ai.id === selectedID} onSelect={() => select(ai.id)} />
          ))}
          {!listQuery.isLoading && items.length === 0 && !creating && (
            <p className="px-1 text-sm text-ink-muted">Nothing built yet.</p>
          )}
        </aside>

        <section className="min-w-0 space-y-4">
          {creating ? (
            <NewAIForm
              onCancel={() => setCreating(false)}
              onCreated={(ai) => {
                void queryClient.invalidateQueries({ queryKey: ['training', 'ais'] })
                select(ai.id)
              }}
            />
          ) : selectedID && viewQuery.data ? (
            <Workspace key={selectedID} view={viewQuery.data} onDeleted={() => select(null)} />
          ) : selectedID && viewQuery.isLoading ? (
            <p className="text-sm text-ink-muted">Loading…</p>
          ) : (
            <>
              <EmptyState
                title="Build a specialized AI"
                description="Describe the job, add examples of good answers, and connect the data it should look up. Yggdrasil recommends a base model, trains it on this computer, and lets you compare it with the original before you use it."
                action={
                  <div className="flex flex-wrap items-center gap-2">
                    <button type="button" className="btn-primary px-3 py-1.5 text-sm" onClick={() => setCreating(true)}>
                      Build a new AI
                    </button>
                    {exampleButton('btn-secondary px-3 py-1.5 text-sm')}
                    <span className="text-xs text-ink-faint">The example uses a sample tire shop, so you can see real data first.</span>
                  </div>
                }
              />
              <ConceptCards />
            </>
          )}
        </section>
      </div>
    </div>
  )
}

function AIListItem({ ai, active, onSelect }: { ai: SpecializedAI; active: boolean; onSelect: () => void }) {
  return (
    <button
      type="button"
      className={['selectable w-full text-left', active ? 'shadow-[inset_0_0_0_1.5px_rgb(var(--rgb-primary))]' : '']
        .filter(Boolean)
        .join(' ')}
      onClick={onSelect}
    >
      <p className="font-medium text-ink">
        {ai.name}
        {ai.example && <span className="status-chip ml-2 bg-info/15 text-info">Example</span>}
      </p>
      <p className="mt-1 text-xs text-ink-muted">
        {ai.deployed_revision > 0 ? `Deployed · revision ${ai.deployed_revision}` : 'Not deployed'}
      </p>
    </button>
  )
}

function NewAIForm({ onCancel, onCreated }: { onCancel: () => void; onCreated: (ai: SpecializedAI) => void }) {
  const [name, setName] = useState('')
  const [goal, setGoal] = useState('')
  const create = useMutation({
    mutationFn: () => api.createAI({ name, goal }),
    onSuccess: (ai) => ai && onCreated(ai),
  })
  return (
    <form
      className="card space-y-4"
      onSubmit={(event) => {
        event.preventDefault()
        create.mutate()
      }}
    >
      <div>
        <h2 className="section-title">What should your AI do?</h2>
        <p className="mt-1 text-sm text-ink-muted">
          Describe the job in a sentence or two, the way you would explain it to a new employee.
        </p>
      </div>
      <label className="block space-y-1">
        <span className="text-sm font-medium text-ink">Name</span>
        <input className="field w-full" value={name} onChange={(e) => setName(e.target.value)} placeholder="Tire shop assistant" required />
      </label>
      <label className="block space-y-1">
        <span className="text-sm font-medium text-ink">The job</span>
        <textarea
          className="field min-h-24 w-full"
          value={goal}
          onChange={(e) => setGoal(e.target.value)}
          placeholder="Answer customer questions about tires. Ask for the vehicle's year, make, and model, explain fitment, and recommend tires we have in stock."
        />
      </label>
      {create.error && <p className="text-sm text-danger">{errorText(create.error)}</p>}
      <div className="flex gap-2">
        <button type="submit" className="btn-primary px-3 py-1.5 text-sm" disabled={!name.trim() || create.isPending}>
          {create.isPending ? 'Creating…' : 'Continue'}
        </button>
        <button type="button" className="btn-secondary px-3 py-1.5 text-sm" onClick={onCancel}>
          Cancel
        </button>
      </div>
    </form>
  )
}

function Workspace({ view, onDeleted }: { view: SpecializedAIView; onDeleted: () => void }) {
  // The example starts at the beginning so each step's note can be read in order.
  const [step, setStep] = useState<StepID>(() => (view.example && view.jobs.length === 0 ? 'describe' : nextStep(view)))
  const done = completedSteps(view)
  const active = view.jobs.find((j) => !isTerminal(j.state))
  const goNext = () => {
    const i = steps.findIndex((s) => s.id === step)
    if (i >= 0 && i < steps.length - 1) setStep(steps[i + 1].id)
  }

  return (
    <div className="space-y-4">
      <div className="card space-y-3 !p-4">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div>
            <h2 className="font-display text-xl font-semibold text-ink">{view.name}</h2>
            <p className="text-xs text-ink-muted">
              {view.base_model ? `${view.base_model.display_name} base` : 'No base model yet'}
              {view.deployed_revision > 0 && (
                <>
                  {' · '}
                  <span className="mono-id">{view.model_id}</span>
                </>
              )}
            </p>
          </div>
          {active && (
            <button type="button" className="status-chip bg-info/15 text-info" onClick={() => setStep('train')}>
              <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-info" aria-hidden />
              {stateLabel(active.state)}
            </button>
          )}
        </div>
        <ol className="flex flex-wrap gap-1.5" aria-label="Build steps">
          {steps.map((s, i) => (
            <li key={s.id}>
              <button
                type="button"
                aria-current={step === s.id ? 'step' : undefined}
                className={[
                  'rounded-md px-2.5 py-1 text-xs font-medium',
                  step === s.id
                    ? 'bg-primary text-primary-fg'
                    : done.has(s.id)
                      ? 'bg-primary-soft text-primary-active'
                      : 'bg-raised text-ink-muted',
                ].join(' ')}
                onClick={() => setStep(s.id)}
              >
                <span className="tabular-nums">{i + 1}.</span> {s.label}
                {done.has(s.id) && step !== s.id && <span aria-label="done"> ✓</span>}
              </button>
            </li>
          ))}
        </ol>
      </div>

      <ExampleNote view={view} step={step} />
      {step === 'describe' && <DescribeStep view={view} onNext={goNext} onDeleted={onDeleted} />}
      {step === 'base' && <BaseModelStep view={view} onNext={goNext} />}
      {step === 'material' && <MaterialStep view={view} onNext={goNext} />}
      {step === 'examples' && <ExamplesStep view={view} onNext={goNext} />}
      {step === 'plan' && <PlanStep view={view} onStarted={() => setStep('train')} />}
      {step === 'train' && <TrainStep view={view} onNext={goNext} />}
      {step === 'test' && <TestStep view={view} onNext={goNext} />}
      {step === 'deploy' && <DeployStep view={view} />}
    </div>
  )
}
