/**
 * The characters voice chat can show (#1369). They ship with the app as
 * small vector drawings, so there's nothing to download. Keep this list in
 * step with VoiceAvatars in internal/app/voice_avatar.go.
 */
export const AVATARS = ['ratatoskr', 'pebble', 'lumen'] as const
export type AvatarId = (typeof AVATARS)[number]

/** Their names are their own, the same in every language. */
export const AVATAR_NAMES: Record<AvatarId, string> = { ratatoskr: 'Ratatoskr', pebble: 'Pebble', lumen: 'Lumen' }

export function isAvatar(id: string | undefined | null): id is AvatarId {
  return AVATARS.includes(id as AvatarId)
}

/**
 * Follows the answer's loudness as a mouth opening, 0 to 1: it opens
 * quickly and closes a little slower, so it reads as talking rather than
 * flicker, and stays shut through the quiet between words.
 */
export function mouthFollower(level: () => number): () => number {
  let open = 0
  return () => {
    const target = level()
    open += (target - open) * (target > open ? 0.6 : 0.3)
    return open < 0.06 ? 0 : Math.min(1, open)
  }
}

