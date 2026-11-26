import { useEffect, useRef } from 'react'
import { animate } from '@/lib/ratatoskr/loop'
import { Rig, type MascotState } from '@/lib/ratatoskr/rig'
import type { VoiceStatus } from '@/lib/voice/realtime'
import { readScreenshotLaunch } from '@/lib/screenshotMode'
import { mouthFollower, type AvatarId } from './avatars'

/** What the character is doing, from the conversation's status. */
type Mood = 'idle' | 'hearing' | 'thinking' | 'speaking' | 'asleep' | 'upset'

function moodOf(status: VoiceStatus): Mood {
  switch (status) {
    case 'hearing':
      return 'hearing'
    case 'thinking':
      return 'thinking'
    case 'speaking':
      return 'speaking'
    case 'closed':
      return 'asleep'
    case 'failed':
      return 'upset'
    default:
      return 'idle'
  }
}

const MASCOT: Record<Mood, MascotState> = {
  idle: 'idle',
  hearing: 'idle',
  thinking: 'think',
  speaking: 'idle',
  asleep: 'sleep',
  upset: 'error',
}

function wantsStill(): boolean {
  if (readScreenshotLaunch()?.enabled || document.documentElement.dataset.screenshot === '1') return true
  return typeof window.matchMedia === 'function' && window.matchMedia('(prefers-reduced-motion: reduce)').matches
}

/**
 * A character for voice chat whose mouth moves with the spoken answer. It
 * stands beside the status text, never in place of it, so it's hidden from
 * assistive technology. With Reduce Motion, or in screenshots, it's a still
 * picture.
 */
export function VoiceAvatar({
  character,
  status,
  level = () => 0,
  size = 144,
  muted = false,
}: {
  character: AvatarId
  status: VoiceStatus
  /** How loud the answer is right now, 0 to 1. */
  level?: () => number
  size?: number
  muted?: boolean
}) {
  const mood = moodOf(status)
  const className = ['shrink-0 transition-opacity duration-300', muted ? 'opacity-40' : ''].join(' ')
  if (character === 'ratatoskr') return <RatatoskrAvatar mood={mood} level={level} size={size} className={className} />
  return <DrawnAvatar character={character} mood={mood} level={level} size={size} className={className} />
}

function RatatoskrAvatar({ mood, level, size, className }: { mood: Mood; level: () => number; size: number; className: string }) {
  const ref = useRef<SVGSVGElement>(null)
  const rig = useRef<Rig | null>(null)
  const moodRef = useRef(mood)
  const levelRef = useRef(level)
  moodRef.current = mood
  levelRef.current = level

  useEffect(() => {
    const svg = ref.current
    if (!svg) return
    const r = new Rig(svg, MASCOT[moodRef.current], { still: wantsStill() })
    const mouth = mouthFollower(() => levelRef.current())
    r.talk = () => (moodRef.current === 'speaking' ? mouth() : -1)
    rig.current = r
    const stop = animate(r)
    return () => {
      stop()
      r.destroy()
      rig.current = null
    }
  }, [])

  useEffect(() => {
    const r = rig.current
    if (r && r.state !== MASCOT[mood]) r.setState(MASCOT[mood])
  }, [mood])

  return <svg ref={ref} width={size} height={size} aria-hidden="true" focusable="false" data-avatar="ratatoskr" data-avatar-mood={mood} className={className} />
}

interface Look {
  cheek: string
  eye: string
  mouth: string
  /** The character's own shape, behind its face. */
  shape: (glow: number) => React.ReactNode
  eyes: [number, number, number]
  mouthAt: [number, number]
}

const LOOKS: Record<Exclude<AvatarId, 'ratatoskr'>, Look> = {
  // A round river stone with a tuft of moss.
  pebble: {
    cheek: '#e89a9a',
    eye: '#1f2a33',
    mouth: '#3b2a2a',
    eyes: [46, 74, 64],
    mouthAt: [60, 80],
    shape: () => (
      <>
        <path d="M60 32 C 90 32, 100 52, 98 72 C 96 94, 80 104, 60 104 C 40 104, 22 94, 22 72 C 20 50, 32 32, 60 32 Z" fill="#9aaebd" stroke="#5d7080" strokeWidth={3} />
        <path d="M52 34 C 50 26, 56 22, 58 30 C 60 20, 68 22, 64 32 C 70 26, 76 30, 68 35 Z" fill="#6fae5a" stroke="#4d8a3c" strokeWidth={2} strokeLinejoin="round" />
        <ellipse cx={44} cy={50} rx={8} ry={4} fill="#ffffff" opacity={0.25} transform="rotate(-25 44 50)" />
      </>
    ),
  },
  // A little lantern flame with a warm glow.
  lumen: {
    cheek: '#ff8a5c',
    eye: '#4a2b0f',
    mouth: '#7a3410',
    eyes: [50, 70, 74],
    mouthAt: [60, 88],
    shape: (glow) => (
      <>
        <ellipse cx={60} cy={76} rx={44} ry={40} fill="#ffd77a" opacity={0.12 + glow * 0.18} />
        <path d="M60 14 C 76 38, 94 56, 92 80 C 90 100, 74 110, 60 110 C 46 110, 30 100, 28 80 C 26 56, 44 38, 60 14 Z" fill="#ffc857" stroke="#e0951f" strokeWidth={3} strokeLinejoin="round" />
        <path d="M60 46 C 68 58, 78 68, 76 84 C 74 96, 66 102, 60 102 C 54 102, 46 96, 44 84 C 42 68, 52 58, 60 46 Z" fill="#ffe7a3" />
      </>
    ),
  },
}

/** Where a still picture sits for each mood. */
const STILL: Record<Mood, { open: number; blink: number; lookX: number; lookY: number }> = {
  idle: { open: 0, blink: 1, lookX: 0, lookY: 0 },
  hearing: { open: 0, blink: 1.15, lookX: 0, lookY: 0 },
  thinking: { open: 0, blink: 1, lookX: 3, lookY: -3 },
  speaking: { open: 0.45, blink: 1, lookX: 0, lookY: 0 },
  asleep: { open: 0, blink: 0.1, lookX: 0, lookY: 1 },
  upset: { open: 0.15, blink: 0.8, lookX: 0, lookY: 1 },
}

function DrawnAvatar({ character, mood, level, size, className }: { character: Exclude<AvatarId, 'ratatoskr'>; mood: Mood; level: () => number; size: number; className: string }) {
  const look = LOOKS[character]
  const body = useRef<SVGGElement>(null)
  const eyeL = useRef<SVGEllipseElement>(null)
  const eyeR = useRef<SVGEllipseElement>(null)
  const mouthEl = useRef<SVGEllipseElement>(null)
  const moodRef = useRef(mood)
  const levelRef = useRef(level)
  moodRef.current = mood
  levelRef.current = level
  const still = useRef<boolean | null>(null)
  if (still.current === null) still.current = wantsStill()
  const [lx, rx, ey] = look.eyes
  const [mx, my] = look.mouthAt
  const s = STILL[mood]

  useEffect(() => {
    if (still.current || typeof requestAnimationFrame === 'undefined') return
    const mouth = mouthFollower(() => levelRef.current())
    let t = 0
    let last = 0
    let nextBlink = 1.5 + Math.random() * 3
    let blinkT = -1
    let lookX = 0
    let lookY = 0
    let frame = 0
    const draw = (now: number) => {
      const dt = last ? Math.min(0.05, (now - last) / 1000) : 1 / 60
      last = now
      t += dt
      const m = moodRef.current
      // Breathing, quicker while listening closely, slow and deep asleep.
      const rate = m === 'asleep' ? 1.2 : m === 'hearing' ? 3 : 2
      const breath = Math.sin(t * rate) * (m === 'asleep' ? 0.03 : 0.018)
      const lean = m === 'hearing' ? 0.04 : 0
      const bob = m === 'thinking' ? Math.sin(t * 1.6) * 2 : 0
      body.current?.setAttribute('transform', `translate(60 108) scale(${1 + lean + breath} ${1 + lean - breath}) translate(-60 ${-108 + bob})`)
      // Blinks, now and then; eyes shut asleep.
      nextBlink -= dt
      if (nextBlink < 0 && blinkT < 0 && m !== 'asleep') {
        blinkT = 0
        nextBlink = 2.2 + Math.random() * 3.5
      }
      let blink = STILL[m].blink
      if (blinkT >= 0) {
        blinkT += dt
        const b = blinkT / 0.16
        blink *= b < 1 ? Math.abs(1 - 2 * b) : 1
        if (b >= 1) blinkT = -1
      }
      lookX += (STILL[m].lookX - lookX) * 0.1
      lookY += (STILL[m].lookY - lookY) * 0.1
      for (const [el, x] of [
        [eyeL.current, lx],
        [eyeR.current, rx],
      ] as const) {
        el?.setAttribute('cx', String(x + lookX))
        el?.setAttribute('cy', String(ey + lookY))
        el?.setAttribute('ry', String(Math.max(0.6, 5 * blink)))
      }
      const open = m === 'speaking' ? mouth() : STILL[m].open
      mouthEl.current?.setAttribute('ry', String(1.4 + open * 7))
      mouthEl.current?.setAttribute('rx', String(6 + open * 2))
      frame = requestAnimationFrame(draw)
    }
    frame = requestAnimationFrame(draw)
    return () => cancelAnimationFrame(frame)
  }, [lx, rx, ey])

  return (
    <svg width={size} height={size} viewBox="0 0 120 120" aria-hidden="true" focusable="false" data-avatar={character} data-avatar-mood={mood} className={className}>
      <ellipse cx={60} cy={110} rx={30} ry={4} fill="#000000" opacity={0.12} />
      <g ref={body}>
        {look.shape(mood === 'thinking' || mood === 'speaking' ? 1 : 0)}
        <circle cx={lx - 9} cy={ey + 12} r={5} fill={look.cheek} opacity={0.45} />
        <circle cx={rx + 9} cy={ey + 12} r={5} fill={look.cheek} opacity={0.45} />
        <ellipse ref={eyeL} cx={lx + s.lookX} cy={ey + s.lookY} rx={4.5} ry={Math.max(0.6, 5 * s.blink)} fill={look.eye} />
        <ellipse ref={eyeR} cx={rx + s.lookX} cy={ey + s.lookY} rx={4.5} ry={Math.max(0.6, 5 * s.blink)} fill={look.eye} />
        <ellipse ref={mouthEl} cx={mx} cy={my} rx={6 + s.open * 2} ry={1.4 + s.open * 7} fill={look.mouth} />
      </g>
    </svg>
  )
}
