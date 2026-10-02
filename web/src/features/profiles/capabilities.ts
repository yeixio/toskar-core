import type { ToolPolicy } from '@/types/api'

export type CapabilityId = 'internet' | 'files' | 'code' | 'shell' | 'git'

export const CAPABILITIES: {
  id: CapabilityId
  label: string
  description: string
  tools: { id: string; on: ToolPolicy['policy'] }[]
}[] = [
  {
    id: 'internet',
    label: 'Internet',
    description: 'Search and read current information from the web',
    tools: [
      { id: 'internet.search', on: 'allow' },
      { id: 'internet.open', on: 'allow' },
    ],
  },
  {
    id: 'files',
    label: 'Files',
    description: 'Find, read, and write files in the workspace',
    tools: [
      { id: 'filesystem.search', on: 'allow' },
      { id: 'filesystem.read', on: 'allow' },
      { id: 'filesystem.write', on: 'allow' },
    ],
  },
  {
    id: 'code',
    label: 'Run code',
    description: 'Run Python in a sandbox for calculations, analysis, and charts; asks first',
    tools: [{ id: 'code.execute', on: 'ask' }],
  },
  {
    id: 'shell',
    label: 'Shell',
    description: 'Run commands on this computer',
    tools: [{ id: 'terminal', on: 'allow' }],
  },
  {
    id: 'git',
    label: 'Git',
    description: 'Inspect the repository, and commit or push',
    tools: [
      { id: 'git.status', on: 'allow' },
      { id: 'git.diff', on: 'allow' },
      { id: 'git.log', on: 'allow' },
      { id: 'git.show', on: 'allow' },
      { id: 'git.add', on: 'allow' },
      { id: 'git.commit', on: 'allow' },
      { id: 'git.push', on: 'allow' },
    ],
  },
]

export function capabilityEnabled(tools: ToolPolicy[] | undefined, id: CapabilityId): boolean {
  const capability = CAPABILITIES.find((item) => item.id === id)
  if (!capability) return false
  return capability.tools.some((tool) => {
    const policy = tools?.find((row) => row.tool_id === tool.id)?.policy
    return policy != null && policy !== 'deny'
  })
}

export function setCapability(tools: ToolPolicy[], id: CapabilityId, enabled: boolean): ToolPolicy[] {
  const capability = CAPABILITIES.find((item) => item.id === id)
  if (!capability) return tools
  const next = tools.map((tool) => ({ ...tool }))
  for (const spec of capability.tools) {
    const policy = enabled ? spec.on : 'deny'
    const row = next.find((tool) => tool.tool_id === spec.id)
    if (row) row.policy = policy
    else next.push({ tool_id: spec.id, policy })
  }
  return next
}

export function activeCapabilityLabels(tools: ToolPolicy[] | undefined): string[] {
  return CAPABILITIES.filter((item) => capabilityEnabled(tools, item.id)).map((item) => item.label)
}
