/** Whether a reasoning model thinks before it answers (#840): on, or off to answer straight away. */
export type Thinking = 'on' | 'off'

/**
 * The thinking a chat sends for its choice: only a change from the model's
 * own way (#593). A model that answers first is asked to think with on; the
 * others are asked not to with off. A model that doesn't think gets nothing.
 */
export function thinkingToSend(thinks: boolean, answersFirst: boolean, choice: Thinking): Thinking | undefined {
  if (!thinks) return undefined
  if (answersFirst) return choice === 'on' ? 'on' : undefined
  return choice === 'off' ? 'off' : undefined
}
