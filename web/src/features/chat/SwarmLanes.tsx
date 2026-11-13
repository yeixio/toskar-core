import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ToolPermissionPrompt, type PendingToolPrompt } from './ToolPermissionPrompt'
import { toolDisplayName } from './toolNames'
import { type AgentApproval, type Lane, type SwarmView, laneActive, swarmActive } from './swarmState'

const STATUS_MARK: Record<string, { mark: string; tone: string }> = {
  queued: { mark: '○', tone: 'text-ink-faint' },
  running: { mark: '●', tone: 'text-norn' },
  done: { mark: '✓', tone: 'text-success' },
  failed: { mark: '!', tone: 'text-warning' },
  stopped: { mark: '■', tone: 'text-ink-faint' },
  out_of_budget: { mark: '!', tone: 'text-warning' },
}

const KNOWN_STATUSES = Object.keys(STATUS_MARK)

/**
 * A swarm's lanes in its coordinator's chat (#1135): one per agent, with its
 * part, computer, model, status, latest steps, and result, and Stop and
 * Open. Then how the merge is going, and in a task where the result went.
 * Live while the swarm runs; folded above its answer after.
 */
export function SwarmLanes({
  view,
  defaultOpen,
  nodeName,
  modelName,
  onStop,
  onOpen,
}: {
  view: SwarmView
  defaultOpen: boolean
  nodeName: (id: string) => string
  modelName: (id: string) => string
  /** Stops one agent: its own chat's turn. */
  onStop: (conversationId: string) => void
  onOpen: (conversationId: string) => void
}) {
  const { t } = useTranslation('chat')
  const [open, setOpen] = useState(defaultOpen)
  const active = swarmActive(view.status)
  const count = view.lanes.length
  const mode = view.mode === 'race' ? t('swarm.modes.race') : t('swarm.modes.split')
  const heading = active ? t('swarm.working', { count, mode }) : t('swarm.worked', { count, mode })
  const listId = `swarm-${view.id}-lanes`

  return (
    <section className="chat-aside space-y-2 border-s-2 border-norn/40 ps-3" aria-label={t('swarm.label')}>
      <button
        type="button"
        className="text-start text-sm font-medium text-ink underline-offset-2 hover:underline"
        aria-expanded={open}
        aria-controls={listId}
        onClick={() => setOpen((v) => !v)}
      >
        {heading} <span aria-hidden>{open ? '▾' : '▸'}</span>
      </button>
      {open ? (
        <div id={listId} className="space-y-2">
          <ol className="space-y-2">
            {view.lanes.map((lane) => (
              <SwarmLane
                key={lane.agent}
                lane={lane}
                winner={view.winner === lane.agent}
                nodeName={nodeName}
                modelName={modelName}
                onStop={onStop}
                onOpen={onOpen}
              />
            ))}
          </ol>
          <SwarmOutcome view={view} />
        </div>
      ) : null}
    </section>
  )
}

function SwarmLane({
  lane,
  winner,
  nodeName,
  modelName,
  onStop,
  onOpen,
}: {
  lane: Lane
  winner: boolean
  nodeName: (id: string) => string
  modelName: (id: string) => string
  onStop: (conversationId: string) => void
  onOpen: (conversationId: string) => void
}) {
  const { t } = useTranslation('chat')
  const status = KNOWN_STATUSES.includes(lane.status) ? lane.status : 'running'
  const { mark, tone } = STATUS_MARK[status]
  const name = lane.part ? t('swarm.agentPart', { n: lane.agent, part: lane.part }) : t('swarm.agent', { n: lane.agent })
  const where = [lane.nodeId ? nodeName(lane.nodeId) : '', lane.modelId ? modelName(lane.modelId) : ''].filter(Boolean).join(' · ')
  return (
    <li className="text-sm text-ink-muted" aria-label={name}>
      <div className="flex items-start gap-2">
        <span aria-hidden className={`mt-px ${tone}`}>
          {mark}
        </span>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-baseline gap-x-2">
            <span className={`line-clamp-2 min-w-0 break-words font-medium ${status === 'running' ? 'text-ink' : 'text-ink-muted'}`}>{name}</span>
            <span className="shrink-0 text-xs">{t(`swarm.status.${status}`)}</span>
            {winner ? <span className="shrink-0 text-xs font-medium text-success">{t('swarm.winner')}</span> : null}
          </div>
          {where ? <p className="truncate text-xs text-ink-faint">{where}</p> : null}
          {lane.steps.length > 0 && laneActive(lane.status) ? (
            <ul className="mt-1 space-y-0.5 text-xs" aria-label={t('swarm.steps')}>
              {lane.steps.map((step) => (
                <li key={step.step} className="truncate">
                  <span className="text-ink">{step.tool ? toolDisplayName(step.tool) : t('tools.unnamed')}</span>
                  {step.summary ? ` — ${step.summary}` : ''}
                </li>
              ))}
            </ul>
          ) : null}
          {lane.summary && !laneActive(lane.status) ? (
            <p className="mt-1 line-clamp-3 whitespace-pre-line break-words text-xs">{lane.summary}</p>
          ) : null}
          {lane.branch && !laneActive(lane.status) ? (
            <p className="mt-0.5 truncate font-mono text-[11px] text-ink-faint" title={lane.branch}>
              {lane.branch}
            </p>
          ) : null}
          <div className="mt-1 flex flex-wrap gap-3 text-xs">
            {laneActive(lane.status) ? (
              <button
                type="button"
                className="font-medium text-ink-muted underline-offset-2 hover:text-ink hover:underline"
                aria-label={t('swarm.stopAgent', { agent: name })}
                onClick={() => onStop(lane.conversationId)}
              >
                {t('swarm.stop')}
              </button>
            ) : null}
            {lane.conversationId ? (
              <button
                type="button"
                className="font-medium text-primary underline-offset-2 hover:underline"
                aria-label={t('swarm.openAgent', { agent: name })}
                onClick={() => onOpen(lane.conversationId)}
              >
                {t('swarm.open')}
              </button>
            ) : null}
          </div>
        </div>
      </div>
    </li>
  )
}

/** How the merge went, and in a task where the result is. */
function SwarmOutcome({ view }: { view: SwarmView }) {
  const { t } = useTranslation('chat')
  const lines: { key: string; text: string; tone?: string }[] = []
  if (view.status === 'merging') {
    lines.push({
      key: 'merging',
      text: view.merging?.agent ? t('swarm.mergingAgent', { n: view.merging.agent }) : t('swarm.merging'),
      tone: 'text-norn',
    })
  }
  for (const c of view.conflicts) {
    lines.push({
      key: `conflict-${c.agent}`,
      text: t('swarm.conflict', { n: c.agent, files: c.files.join(', ') || '—', branch: c.branch }),
      tone: 'text-warning',
    })
  }
  if (!swarmActive(view.status)) {
    if (view.status === 'stopped') lines.push({ key: 'stopped', text: t('swarm.stoppedAll') })
    if (view.status === 'failed') lines.push({ key: 'failed', text: t('swarm.failedAll'), tone: 'text-warning' })
    if (view.branch) lines.push({ key: 'branch', text: t('swarm.branch', { branch: view.branch }) })
    if (view.checks === 'passed') lines.push({ key: 'checks', text: t('swarm.checksPassed'), tone: 'text-success' })
    if (view.checks === 'failed') lines.push({ key: 'checks', text: t('swarm.checksFailed'), tone: 'text-warning' })
    if (view.folder && view.branch) {
      lines.push({
        key: 'applied',
        text: view.applied ? t('swarm.applied') : t('swarm.notApplied', { branch: view.branch }),
      })
    }
  }
  if (lines.length === 0) return null
  return (
    <ul className="space-y-0.5 text-xs text-ink-muted" role="status">
      {lines.map((line) => (
        <li key={line.key} className={`break-words ${line.tone ?? ''}`}>
          {line.text}
        </li>
      ))}
    </ul>
  )
}

/**
 * Questions a swarm's agents are waiting on, in the coordinator's chat,
 * each labelled by its agent. Allow for this agent covers only that
 * agent's chat (#1135).
 */
export function SwarmApprovals({
  approvals,
  toPrompt,
  showRawArgs,
  onDecide,
}: {
  approvals: AgentApproval[]
  toPrompt: (ask: AgentApproval) => PendingToolPrompt
  showRawArgs: boolean
  onDecide: (ask: AgentApproval, allow: boolean, allowSession: boolean) => Promise<void>
}) {
  const { t } = useTranslation('chat')
  const [deciding, setDeciding] = useState<string | null>(null)
  if (approvals.length === 0) return null
  return (
    <ul className="chat-aside space-y-3" aria-label={t('swarm.approvals')}>
      {approvals.map((ask) => (
        <li key={ask.request_id} className="card space-y-3">
          <ToolPermissionPrompt
            tool={toPrompt(ask)}
            toolName={toolDisplayName(ask.tool_id)}
            showRawArgs={showRawArgs}
            deciding={deciding === ask.request_id}
            agent={ask.agent || (ask.agent_number ? t('swarm.agent', { n: ask.agent_number }) : t('swarm.anAgent'))}
            titleId={`swarm-ask-${ask.request_id}`}
            onDecide={(allow, allowSession) => {
              setDeciding(ask.request_id)
              void onDecide(ask, allow, allowSession).finally(() => setDeciding(null))
            }}
          />
        </li>
      ))}
    </ul>
  )
}
