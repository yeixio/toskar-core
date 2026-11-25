import type { Model, Recommendation } from '@/types/api'

/**
 * A chat model already on this computer, such as one added from a file,
 * that setup can use instead of downloading (#1214): none when the
 * recommendation needs no download. The largest is the most capable.
 */
export function modelOnHand(rec: Recommendation, models: Model[]): Model | null {
  if (rec.models.length > 0 && rec.models.every((m) => m.installed)) return null
  const here = models.filter((m) => m.installed && !m.support_role)
  if (here.length === 0) return null
  return [...here].sort((a, b) => (b.size_bytes ?? 0) - (a.size_bytes ?? 0))[0]
}

/**
 * The recommendation with the model on hand in every chat role, and only
 * the supporting models that are here too, so nothing is downloaded and
 * setup works offline.
 */
export function withModelOnHand(rec: Recommendation, model: Model): Recommendation {
  const support = rec.models.filter((m) => m.support_role && m.installed)
  const kept = new Set([model.id, ...support.map((m) => m.id)])
  const chat = new Set(rec.models.filter((m) => !m.support_role).map((m) => m.id))
  const roles = rec.roles
    .map((role) => (chat.has(role.model_id) || !rec.models.some((m) => m.id === role.model_id) ? { ...role, model_id: model.id } : role))
    .filter((role) => kept.has(role.model_id))
  return {
    ...rec,
    models: [model, ...support],
    roles: roles.filter((role, i) => roles.findIndex((r) => r.role === role.role) === i),
    estimated_storage_bytes: 0,
  }
}
