import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import type { Capability } from '@/types/api'

/**
 * What the signed-in person may use (#589), for hiding what's off; the
 * service refuses it anyway. Until /me answers, and from a service from
 * before capabilities, everything is allowed.
 */
export function useCapabilities(): { allows: (c: Capability) => boolean } {
  const me = useQuery({ queryKey: ['me'], queryFn: () => api.getMe(), staleTime: 60_000, retry: false })
  const caps = me.data?.capabilities
  return { allows: (c) => caps?.[c] !== false }
}
