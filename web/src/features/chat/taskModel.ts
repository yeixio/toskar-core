import type { FitLabel, Model, ModelsFitResponse } from '@/types/api'

/** Marks a catalog model measured to finish tasks, as toskarctl chat's suggestion uses it (#1034). */
const taskModelTag = 'tasks'

/**
 * The model to suggest for work in a chat's folder, when nothing installed is
 * measured to finish tasks: smaller models often can't (#1017). Its fit is on
 * the computer Toskar runs on, the first of /models/fit, since a task's
 * commands run there. Null when one is installed, or the catalog has none.
 */
export function taskModelSuggestion(
  models: Model[],
  fits: ModelsFitResponse[] | undefined,
): { model: Model; fit?: FitLabel; runsHere: boolean } | null {
  const tagged = models.filter((m) => m.tags?.includes(taskModelTag))
  if (tagged.length === 0 || tagged.some((m) => m.installed)) return null
  const here = fits?.[0]?.fits ?? []
  const fitOf = (m: Model) => here.find((f) => f.model_id === m.id)?.label ?? m.fit?.label
  const runs = (label?: FitLabel) => !!label && label !== 'too_large' && label !== 'unsupported'
  const model = tagged.find((m) => runs(fitOf(m))) ?? tagged[0]
  const fit = fitOf(model)
  return { model, fit, runsHere: runs(fit) }
}
