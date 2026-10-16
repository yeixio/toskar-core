import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'

/** Where the published user guide lives. */
const guideBase = 'https://toskar.ai/docs'

/**
 * The address of a user guide section: the guide for this release, when it
 * is one, so the page matches what's running, else the latest guide.
 */
export function guideUrl(section: string, version?: string): string {
  const release = version && /^\d+\.\d+\.\d+$/.test(version) ? `/${version}` : ''
  return `${guideBase}${release}#${section}`
}

/** guideUrl for the version this Toskar runs. */
export function useGuideUrl(section: string): string {
  const version = useQuery({ queryKey: ['version'], queryFn: () => api.getVersion(), retry: false, staleTime: Infinity })
  return guideUrl(section, version.data?.version)
}
