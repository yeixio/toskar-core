import type { NetworkAccess } from '@/types/api'

/** Whether anyone at all could connect from another computer (#711): the server's own rule. */
export function anyoneCanConnect(n: NetworkAccess): boolean {
  return n.people > 0 || n.devices > 0 || n.keys > 0 || n.sign_in_service
}
