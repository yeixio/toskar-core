import type { TFunction } from 'i18next'
import { bytesToGb } from '@/lib/format'
import type { Recommendation } from '@/types/api'

/** "macOS, Apple silicon" for darwin/arm64, instead of Go's names (#1346). */
export function systemName(os: string, arch: string | undefined, t: TFunction): string {
  const arm = arch === 'arm64' || arch === 'arm'
  if (os === 'darwin') return t(arm ? 'hardware.os.macAppleSilicon' : 'hardware.os.macIntel')
  if (os === 'windows') return t(arm ? 'hardware.os.windowsArm' : 'hardware.os.windows')
  if (os === 'linux') return t(arm ? 'hardware.os.linuxArm' : 'hardware.os.linux')
  return arch ? `${os} / ${arch}` : os
}

/** Why the setup fits, in the memory figure the card above shows (#1346). */
export function fitsLine(recommendation: Recommendation, memory: number | undefined, t: TFunction): string {
  const primary = recommendation.models.find((m) => m.id === recommendation.roles[0]?.model_id) ?? recommendation.models[0]
  if (!primary || !memory) return recommendation.reason
  return t('recommend.fits', { name: primary.display_name, memory: bytesToGb(memory) })
}
