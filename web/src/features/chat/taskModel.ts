import type { Model } from '@/types/api'

/** Marks a catalog model measured to finish tasks, as toskarctl chat's suggestion uses it (#1034). */
const taskModelTag = 'tasks'

/**
 * The model to suggest for work in a chat's folder, when nothing installed is
 * measured to finish tasks: smaller models often can't (#1017). Null when one
 * is installed, or the catalog has none.
 */
export function taskModelSuggestion(models: Model[]): { model: Model; runsHere: boolean } | null {
  const tagged = models.filter((m) => m.tags?.includes(taskModelTag))
  if (tagged.length === 0 || tagged.some((m) => m.installed)) return null
  const runs = (m: Model) => !!m.fit?.label && m.fit.label !== 'too_large' && m.fit.label !== 'unsupported'
  const model = tagged.find(runs) ?? tagged[0]
  return { model, runsHere: runs(model) }
}
