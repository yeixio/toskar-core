import { Trans, useTranslation } from 'react-i18next'

/**
 * The line under an agent's chat's header (#1135): which agent of a swarm
 * it is, with a way back to the chat the swarm was started in.
 */
export function SwarmAgentLine({ agent, onOpenCoordinator }: { agent: number; onOpenCoordinator: () => void }) {
  const { t } = useTranslation('chat')
  return (
    <p className="flex shrink-0 flex-wrap items-center justify-center gap-x-1 border-b border-line/40 px-4 py-0.5 text-xs text-ink-muted">
      <Trans
        t={t}
        i18nKey="swarm.agentLine"
        values={{ n: agent }}
        components={{
          go: (
            <button
              type="button"
              className="inline-flex min-h-6 items-center font-medium text-ink underline-offset-2 hover:text-primary-active hover:underline"
              onClick={onOpenCoordinator}
            />
          ),
        }}
      />
    </p>
  )
}
