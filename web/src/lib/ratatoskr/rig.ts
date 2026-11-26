/**
 * Ratatoskr, the Yggdrasil mascot: an SVG squirrel drawn in code.
 *
 * A port of the `Squirrel` class in docs/brand/mascot/ratatoskr-prototype.html.
 * The geometry, colors, poses, and easing match the prototype; see
 * docs/brand/mascot/README.md for how the rig works. Framework-free: a
 * component creates a rig on an <svg>, calls setState, steps it from the
 * shared loop, and destroys it on unmount.
 */

export type MascotState = 'idle' | 'greet' | 'think' | 'deliver' | 'success' | 'error' | 'sleep'

export const MASCOT_STATES: MascotState[] = ['idle', 'greet', 'think', 'deliver', 'success', 'error', 'sleep']

const NS = 'http://www.w3.org/2000/svg'
const DEG = Math.PI / 180

/** Mascot colors. Copper fur reads on both themes; don't recolor him. */
const C = {
  out: '#2A1710',
  fur: '#C45E31',
  furDk: '#8E3E1C',
  furLt: '#E89257',
  cream: '#F5E3C8',
  eye: '#13202A',
  teal: '#4FD1C5',
  tealDk: '#13756E',
  gold: '#D1A962',
  cap: '#6A4325',
  capDk: '#4A2E19',
  strap: '#4B3022',
  blush: '#F07A62',
  drop: '#7FC6E8',
}

type Pose = {
  bodyY: number
  sx: number
  sy: number
  lean: number
  headRot: number
  headY: number
  earL: number
  earR: number
  open: number
  happy: number
  closed: number
  worry: number
  pupX: number
  pupY: number
  mouth: number
  blush: number
  pLx: number
  pLy: number
  pRx: number
  pRy: number
  sRx: number
  sRy: number
  ax: number
  ay: number
  ar: number
  glow: number
  tailA: number
  curl0: number
  curl1: number
  swayAmp: number
  swayF: number
  dots: number
  speed: number
  sweat: number
}

const BASE: Pose = {
  bodyY: 0, sx: 1, sy: 1, lean: 0, headRot: 0, headY: 0, earL: 0, earR: 0, open: 1, happy: 0, closed: 0, worry: 0,
  pupX: 0, pupY: 0, mouth: 0, blush: 0.35, pLx: 95, pLy: 127, pRx: 109, pRy: 127, sRx: 112, sRy: 117, ax: 102, ay: 127, ar: 0, glow: 0.2,
  tailA: -164, curl0: 5.6, curl1: 36, swayAmp: 2.2, swayF: 1.5, dots: 0, speed: 0, sweat: 0,
}

type PoseFn = (t: number, rig: { enter: { ax: number; ay: number } }) => Partial<Pose>

const POSE: Record<MascotState, PoseFn> = {
  idle: (t) => ({ bodyY: Math.sin(t * 2.1) * 0.8, sy: 1 + Math.sin(t * 2.1) * 0.012, headRot: Math.sin(t * 0.6) * 3, pupX: Math.sin(t * 0.45) * 1.4 }),
  greet: (t) => {
    const w = Math.sin(t * 9)
    const a = (-52 + w * 14) * DEG
    return {
      headRot: 7 + Math.sin(t * 2) * 1.5, bodyY: -Math.abs(Math.sin(t * 3)) * 1.6, sRx: 118, sRy: 110,
      pRx: 118 + Math.cos(a) * 29, pRy: 110 + Math.sin(a) * 29, pLx: 99, pLy: 130, ax: 99, ay: 130, ar: -12,
      mouth: 0.55, blush: 0.65, earR: 4 + w * 3, pupX: 1.6, pupY: -0.5, swayAmp: 4.5, swayF: 3,
    }
  },
  think: (t) => {
    const c = t % 3.4
    const nib = c < 2.2
    const j = nib ? Math.sin(t * 30) : 0
    return {
      ax: 102 + j * 0.3, ay: nib ? 100 + j * 0.8 : 110, ar: nib ? Math.sin(t * 15) * 5 : -10,
      pLx: 95, pLy: nib ? 103 : 112, pRx: 109, pRy: nib ? 103 : 112,
      mouth: nib ? (j > 0 ? 0.7 : 0.2) : 0, headRot: nib ? -3 : 6, pupX: nib ? 0 : 2.2, pupY: nib ? 3 : -3, earL: nib ? 0 : -7, earR: nib ? 0 : 3,
      dots: 1, glow: 0.45, swayAmp: 2, swayF: 1.2, blush: 0.45,
    }
  },
  deliver: (t) => {
    const p = (t * 2.2) % 1
    const h = Math.sin(p * Math.PI)
    const g = Math.pow(1 - h, 8)
    return {
      bodyY: -h * 13, sy: 1 + 0.07 * h - 0.1 * g, sx: 1 - 0.04 * h + 0.07 * g, lean: 7, earL: -10 - 8 * h, earR: 6 + 8 * h, headRot: 4,
      pupX: 2.6, pupY: -0.5, mouth: 0.25, speed: 1, glow: 0.8 + 0.2 * Math.sin(t * 9), tailA: -172, curl0: 7, curl1: 15,
      swayAmp: 6, swayF: 8, ay: 124,
    }
  },
  success: (t) => {
    const p = (t % 1.8) / 1.8
    let bodyY = 0
    let sx = 1
    let sy = 1
    let air = false
    let q = 0
    if (p < 0.18) {
      sy = 0.9
      sx = 1.05
    } else if (p < 0.62) {
      q = (p - 0.18) / 0.44
      bodyY = -26 * Math.sin(Math.PI * q)
      sy = 1.07
      sx = 0.96
      air = true
    } else if (p < 0.72) {
      sy = 0.92
      sx = 1.05
    }
    const o: Partial<Pose> = { bodyY, sx, sy, happy: 1, mouth: 0.8, blush: 0.7, glow: 0.9, tailA: -150, curl0: 6, curl1: 17, swayAmp: 6, swayF: 5 }
    if (air) Object.assign(o, { pLx: 80, pLy: 98, pRx: 124, pRy: 98, ax: 102, ay: 124 - 82 * Math.sin(Math.PI * q), ar: 360 * q, earL: -6, earR: 6 })
    else Object.assign(o, { ay: 124, pLy: 124, pRy: 124, ar: 0 })
    return o
  },
  error: (t, s) => {
    const y0 = s.enter.ay
    const x0 = s.enter.ax
    const f = Math.min(t / 0.5, 1)
    let ay = y0 + (171 - y0) * f * f
    let ax = x0
    let ar = 0
    if (t > 0.5) {
      const r = Math.min((t - 0.5) / 1.1, 1)
      const e = 1 - Math.pow(1 - r, 3)
      ax = x0 + (142 - x0) * e
      ar = e * 320
      ay = 171 - Math.abs(Math.sin(e * Math.PI * 2)) * 7 * (1 - e)
    }
    return {
      ax, ay, ar, sy: 0.97, headRot: -7 + Math.sin(t * 1.2) * 1.2, headY: 2, earL: -38, earR: 38, worry: 1,
      pupX: 2.6, pupY: 3, pLx: 92, pLy: 132, pRx: 112, pRy: 132, blush: 0, sweat: 1, glow: 0,
      tailA: -174, curl0: 5, curl1: 14, swayAmp: 0.8, swayF: 0.8,
    }
  },
  sleep: (t) => ({
    bodyY: 2, sy: 1 + Math.sin(t * 1.3) * 0.025, sx: 1 - Math.sin(t * 1.3) * 0.01, headRot: 9, headY: 7, closed: 1, open: 0,
    earL: -16, earR: 12, blush: 0.55, tailA: -146, curl0: 7, curl1: 52, swayAmp: 0.6, swayF: 0.6,
    ax: 102, ay: 131, pLx: 96, pLy: 131, pRx: 108, pRy: 131, glow: 0.1,
  }),
}

const RATE: Partial<Record<MascotState, number>> = { success: 24, deliver: 20, error: 14 }
const DIRECT: Partial<Record<MascotState, (keyof Pose)[]>> = {
  error: ['ax', 'ay', 'ar'],
  success: ['ax', 'ay', 'ar', 'bodyY', 'sx', 'sy'],
}

/**
 * The moment a still frame shows. The prototype's reduced-motion frame is
 * t = 0.4; error holds its final pose (acorn on the ground) and success is
 * caught mid-jump, so each still reads as its state.
 */
const STILL_T: Record<MascotState, number> = { idle: 0.4, greet: 0.4, think: 0.4, deliver: 0.25, success: 0.75, error: 3, sleep: 0.4 }

const EAR = `<path d="M77 54 C70 41 68 27 72 14 C82 20 92 33 95 45 Z" fill="${C.fur}" stroke="${C.out}" stroke-width="2.4" stroke-linejoin="round"/>
  <path d="M80.5 47 C76.5 38 75.5 29 76.5 22 C82 27 87 35 89 43 Z" fill="${C.furDk}"/>
  <path d="M72 16 C70 10 67 5 62 1 C67.5 2 71 4 73 7 C73 2.5 74.5 -.5 77.5 -3 C78.5 2.5 78.5 8.5 76.5 15 Z" fill="${C.furDk}" stroke="${C.out}" stroke-width="1.8" stroke-linejoin="round"/>`

function eye(cx: number, cy: number): string {
  return `<g class="eye" data-cx="${cx}" data-cy="${cy}">
    <g class="eo"><ellipse cx="${cx}" cy="${cy}" rx="7.4" ry="8.8" fill="${C.eye}"/>
      <g class="iris"><ellipse cx="${cx}" cy="${cy + 2.2}" rx="5" ry="5.4" fill="${C.teal}" opacity=".9"/>
      <ellipse cx="${cx}" cy="${cy + 2.4}" rx="3" ry="3.5" fill="${C.eye}"/></g>
      <circle cx="${cx - 2.6}" cy="${cy - 3.4}" r="2.7" fill="#fff"/><circle cx="${cx + 2.6}" cy="${cy + 4}" r="1.1" fill="#fff" opacity=".85"/></g>
    <path class="eh" d="M${cx - 6} ${cy + 2.5} Q${cx} ${cy - 6} ${cx + 6} ${cy + 2.5}" fill="none" stroke="${C.eye}" stroke-width="3" stroke-linecap="round" opacity="0"/>
    <path class="ec" d="M${cx - 6.2} ${cy + 0.5} Q${cx} ${cy + 5.5} ${cx + 6.2} ${cy + 0.5}" fill="none" stroke="${C.eye}" stroke-width="2.8" stroke-linecap="round" opacity="0"/>
  </g>`
}

const ANSUZ = 'M0 -6.5 V6.5 M0 -6.5 L5 -2.5 M0 -1.5 L5 2.5'

type Particle = { el: SVGElement; kind: 'z' | 'crumb' | 'spark'; x: number; y: number; vx: number; vy: number; sc: number; life: number; max: number }

export interface RigOptions {
  /** Particles: sparks, crumbs, and z's. Off below 48 px. */
  fx?: boolean
  /** Blinks and ear twitches. Off below 48 px. */
  blinks?: boolean
  /** Render one fixed frame per state and never animate: reduced motion and screenshots. */
  still?: boolean
}

let uid = 0

export class Rig {
  readonly svg: SVGSVGElement
  readonly still: boolean
  state: MascotState = 'idle'
  enter = { ax: BASE.ax, ay: BASE.ay }
  /** How open his mouth is, 0 to 1, while he talks in voice chat (#1369); below 0 the state's pose decides. */
  talk?: () => number
  private readonly id: string
  private readonly fx: boolean
  private readonly blinks: boolean
  private p: Pose = { ...BASE }
  private t = 0
  private phase = 0
  private parts: Particle[] = []
  private nextBlink = 1 + Math.random() * 3
  private blinkT = -1
  private twitch = { ear: 0, t: -1 }
  private nextTwitch = 2 + Math.random() * 4
  private spawnT = 0
  private blink = 1
  private tw = 0
  private el: {
    char: Element
    head: Element
    earL: Element
    earR: Element
    acorn: Element
    glow: Element
    shadow: Element
    limbs: Element[]
    paws: Element[]
    eyes: Element[]
    brows: Element
    mo: Element
    blush: Element
    dots: Element
    speed: Element
    sweat: Element
    fxg: Element
    tl: Element[][]
  }

  constructor(svg: SVGSVGElement, state: MascotState = 'idle', opts: RigOptions = {}) {
    this.svg = svg
    this.id = `ratatoskr${uid++}`
    this.still = Boolean(opts.still)
    this.fx = opts.fx !== false && !this.still
    this.blinks = opts.blinks !== false && !this.still
    svg.setAttribute('viewBox', '-6 -34 212 222')
    svg.innerHTML = this.markup()
    const q = (s: string) => svg.querySelector(s) as Element
    this.el = {
      char: q('.char'), head: q('.head'), earL: q('.earL'), earR: q('.earR'), acorn: q('.acorn'), glow: q('.glow'),
      shadow: q('.shadow'), limbs: [...svg.querySelectorAll('.limb')], paws: [...svg.querySelectorAll('.paw')],
      eyes: [...svg.querySelectorAll('.eye')], brows: q('.brows'), mo: q('.mo'), blush: q('.blushes'),
      dots: q('.dots'), speed: q('.speed'), sweat: q('.sweat'), fxg: q('.fx'),
      tl: [...svg.querySelectorAll('.tail > g')].map((g) => [...g.children]),
    }
    this.setState(state, true)
  }

  private markup(): string {
    const id = this.id
    const N = 18
    const circles = (fill: string) => Array.from({ length: N }, () => `<circle fill="${fill}"/>`).join('')
    return `<defs><radialGradient id="${id}g"><stop offset="0" stop-color="${C.teal}" stop-opacity=".75"/><stop offset="1" stop-color="${C.teal}" stop-opacity="0"/></radialGradient></defs>
    <ellipse class="shadow" cx="100" cy="182" rx="42" ry="5.5" style="fill:var(--ratatoskr-shadow, #000);fill-opacity:var(--ratatoskr-shadow-opacity, .38)"/>
    <g class="speed" opacity="0" stroke="${C.teal}" stroke-width="3" stroke-linecap="round" fill="none" stroke-dasharray="14 30">
      <path d="M8 112 H58"/><path d="M0 130 H50"/><path d="M12 148 H56"/></g>
    <g class="char">
      <g class="tail"><g>${circles(C.out)}</g><g>${circles(C.fur)}</g><g opacity=".5">${circles(C.furDk)}</g><g opacity=".8">${circles(C.furLt)}</g></g>
      <path d="M100 96 C124 96 137 124 135 148 C133 168 119 177 100 177 C81 177 66 168 65 148 C63 124 76 96 100 96 Z" fill="${C.fur}" stroke="${C.out}" stroke-width="2.4"/>
      <path d="M103 114 C116 114 123 132 122 149 C121 164 113 171 103 171 C93 171 85 164 84 149 C83 132 90 114 103 114 Z" fill="${C.cream}"/>
      <path d="M70 156 C73 167 81 173 90 175 M134 154 C131 166 124 172 115 175" fill="none" stroke="${C.furDk}" stroke-width="2" stroke-linecap="round"/>
      <path d="M79 107 C92 128 108 146 125 159" fill="none" stroke="${C.out}" stroke-width="6.6" stroke-linecap="round"/>
      <path d="M79 107 C92 128 108 146 125 159" fill="none" stroke="${C.strap}" stroke-width="3.6" stroke-linecap="round"/>
      <g transform="translate(126 160) rotate(-16)"><rect x="-9.5" y="-6.5" width="19" height="14.5" rx="4" fill="${C.strap}" stroke="${C.out}" stroke-width="2"/>
        <path d="M-9.5 -1 Q0 4.5 9.5 -1" fill="none" stroke="${C.out}" stroke-width="1.6"/><circle cx="0" cy="2.6" r="2" fill="${C.teal}"/></g>
      <ellipse cx="84" cy="177" rx="13.5" ry="6.5" fill="${C.furDk}" stroke="${C.out}" stroke-width="2.2"/>
      <ellipse cx="119" cy="177" rx="13.5" ry="6.5" fill="${C.furDk}" stroke="${C.out}" stroke-width="2.2"/>
      <g class="head">
        <g class="earL">${EAR}</g>
        <g class="earR"><g transform="translate(204 0) scale(-1 1)">${EAR}</g></g>
        <path d="M102 38 C127 38 140 55 139 71 C144 75 145 83 139 86 C135 100 121 108 102 108 C83 108 69 100 65 86 C59 83 60 75 65 71 C64 55 77 38 102 38 Z" fill="${C.fur}" stroke="${C.out}" stroke-width="2.4" stroke-linejoin="round"/>
        <path d="M95 40 C95 32 102 28 109 29 C104 32 102 35 103 39 Z" fill="${C.fur}" stroke="${C.out}" stroke-width="2" stroke-linejoin="round"/>
        <ellipse cx="102" cy="92" rx="16" ry="11" fill="${C.cream}"/>
        <path d="M74 68 C78 62 84 60 90 61 M114 61 C120 60 126 62 130 68" fill="none" stroke="${C.cream}" stroke-width="2.6" stroke-linecap="round" opacity=".55"/>
        <g class="blushes"><ellipse cx="80" cy="88" rx="6" ry="3.4" fill="${C.blush}"/><ellipse cx="124" cy="88" rx="6" ry="3.4" fill="${C.blush}"/></g>
        ${eye(88, 74)}${eye(116, 74)}
        <g class="brows" opacity="0" stroke="${C.out}" stroke-width="2.4" stroke-linecap="round"><path d="M81 63 L93 58.5"/><path d="M111 58.5 L123 63"/></g>
        <ellipse class="mo" cx="102" cy="96.5" rx="4" ry="3.4" fill="#5A1E1A"/>
        <path d="M98.5 86 Q102 84.6 105.5 86 Q104.2 89.8 102 90.4 Q99.8 89.8 98.5 86 Z" fill="${C.out}"/>
        <path d="M96.5 93.5 Q99.3 96.5 102 93.5 Q104.7 96.5 107.5 93.5" fill="none" stroke="${C.out}" stroke-width="1.8" stroke-linecap="round"/>
        <rect x="99.7" y="94.4" width="2.2" height="3.2" rx=".6" fill="#fff" stroke="${C.out}" stroke-width=".8"/>
        <rect x="102.1" y="94.4" width="2.2" height="3.2" rx=".6" fill="#fff" stroke="${C.out}" stroke-width=".8"/>
        <g transform="translate(102 53)" fill="none" stroke-linecap="round" stroke-linejoin="round">
          <path d="${ANSUZ}" stroke="${C.teal}" stroke-width="5" opacity=".22"/><path d="${ANSUZ}" stroke="${C.teal}" stroke-width="2.1"/></g>
        <path class="sweat" d="M142 52 C145 57 147 60 147 62.5 A4 4 0 0 1 139 62.5 C139 60 140 57 142 52 Z" fill="${C.drop}" stroke="${C.out}" stroke-width="1.4" opacity="0"/>
      </g>
      <g class="acorn"><circle class="glow" r="17" fill="url(#${id}g)"/>
        <ellipse cx="0" cy="3" rx="6.6" ry="7.6" fill="${C.gold}" stroke="${C.out}" stroke-width="1.8"/>
        <path d="M-1.4 .8 V7.6 M-1.4 .8 L2 3.4 M-1.4 3.8 L2 6.4" fill="none" stroke="${C.tealDk}" stroke-width="1.3" stroke-linecap="round"/>
        <path d="M-7.9 -.4 C-7.9 -7.6 7.9 -7.6 7.9 -.4 C4 1.6 -4 1.6 -7.9 -.4 Z" fill="${C.cap}" stroke="${C.out}" stroke-width="1.8"/>
        <path d="M-4 -4.2 L-2.2 -1.2 M0 -5.2 L0 -1 M4 -4.2 L2.2 -1.2" stroke="${C.capDk}" stroke-width="1"/>
        <path d="M0 -5.6 Q.8 -8.6 2.6 -9.6" fill="none" stroke="${C.out}" stroke-width="2" stroke-linecap="round"/></g>
      <path class="limb" fill="none" stroke="${C.out}" stroke-width="11" stroke-linecap="round"/>
      <path class="limb" fill="none" stroke="${C.out}" stroke-width="11" stroke-linecap="round"/>
      <path class="limb" fill="none" stroke="${C.fur}" stroke-width="6.6" stroke-linecap="round"/>
      <path class="limb" fill="none" stroke="${C.fur}" stroke-width="6.6" stroke-linecap="round"/>
      <circle class="paw" r="3.6" fill="${C.furLt}"/><circle class="paw" r="3.6" fill="${C.furLt}"/>
    </g>
    <g class="dots" opacity="0" fill="${C.teal}"><circle cx="134" cy="22" r="3.2"/><circle cx="144" cy="22" r="3.2"/><circle cx="154" cy="22" r="3.2"/></g>
    <g class="fx"></g>`
  }

  /** Change state. The pose eases toward the new state; instant snaps to it. */
  setState(s: MascotState, instant = false): void {
    this.state = s
    this.t = 0
    this.enter = { ax: this.p.ax, ay: this.p.ay }
    this.parts.forEach((pt) => pt.el.remove())
    this.parts = []
    if (this.still) {
      // A still frame is the state's pose at a fixed moment, from rest.
      this.enter = { ax: BASE.ax, ay: BASE.ay }
      this.t = STILL_T[s]
      this.p = { ...BASE, ...POSE[s](this.t, this) }
      this.blink = 1
      this.tw = 0
      this.render()
      return
    }
    if (instant) {
      this.p = { ...BASE, ...POSE[s](0, this) }
      this.render()
    }
  }

  /** Advance by dt seconds and draw. Still rigs do not move. */
  step(dt: number): void {
    if (this.still) return
    this.t += dt
    const s = this.state
    const target: Pose = { ...BASE, ...POSE[s](this.t, this) }
    const k = 1 - Math.exp(-dt * (RATE[s] ?? 9))
    const direct = DIRECT[s] ?? []
    for (const key of Object.keys(target) as (keyof Pose)[]) {
      this.p[key] = direct.includes(key) && this.t > 0.08 ? target[key] : this.p[key] + (target[key] - this.p[key]) * k
    }
    const open = this.talk?.() ?? -1
    if (open >= 0) this.p.mouth = open
    this.phase += dt * this.p.swayF
    // Blinking and ear twitches.
    if (this.blinks) {
      this.nextBlink -= dt
      if (this.nextBlink < 0) {
        this.blinkT = 0
        this.nextBlink = 2.2 + Math.random() * 3.5
      }
      this.nextTwitch -= dt
      if (this.nextTwitch < 0) {
        this.twitch = { ear: Math.random() < 0.5 ? -1 : 1, t: 0 }
        this.nextTwitch = 3 + Math.random() * 5
      }
    }
    let blink = 1
    if (this.blinkT >= 0) {
      this.blinkT += dt
      const b = this.blinkT / 0.16
      blink = b < 1 ? Math.abs(1 - 2 * b) : 1
      if (b >= 1) this.blinkT = -1
    }
    let tw = 0
    if (this.twitch.t >= 0) {
      this.twitch.t += dt
      const b = this.twitch.t / 0.22
      tw = b < 1 ? Math.sin(b * Math.PI) * 12 : 0
      if (b >= 1) this.twitch.t = -1
    }
    if (s === 'sleep' || s === 'error') tw = 0
    this.blink = blink
    this.tw = tw
    if (this.fx) this.particles(dt)
    this.render()
  }

  /** Remove everything the rig drew. */
  destroy(): void {
    this.parts.forEach((pt) => pt.el.remove())
    this.parts = []
    this.svg.innerHTML = ''
  }

  private particles(dt: number): void {
    const p = this.p
    const s = this.state
    this.spawnT -= dt
    if (s === 'sleep' && this.spawnT < 0) {
      this.spawn('z', 132, 40, 6, -12)
      this.spawnT = 1.3
    }
    if (s === 'think' && this.spawnT < 0 && this.t % 3.4 < 2.2) {
      this.spawn('crumb', 98 + Math.random() * 8, 108, (Math.random() - 0.5) * 20, -10)
      this.spawnT = 0.35
    }
    if (s === 'success') {
      const ph = (this.t % 1.8) / 1.8
      if (ph > 0.38 && ph < 0.45 && this.spawnT < 0) {
        for (let i = 0; i < 9; i++) {
          const a = -Math.PI / 2 + (i - 4) * 0.36
          this.spawn('spark', 102, 28 + p.bodyY, Math.cos(a) * 70, Math.sin(a) * 70)
        }
        this.spawnT = 1
      }
    }
    if (s === 'deliver' && this.spawnT < 0) {
      this.spawn('spark', 70, 100 + Math.random() * 40 + p.bodyY, -60, 0, 0.6)
      this.spawnT = 0.28
    }
    for (const pt of this.parts) {
      pt.life += dt
      pt.x += pt.vx * dt
      pt.y += pt.vy * dt
      if (pt.kind === 'crumb') {
        pt.vy += 260 * dt
        if (pt.y > 180) pt.life = 99
      }
      if (pt.kind === 'spark') {
        pt.vx *= 0.96
        pt.vy *= 0.96
      }
      const a = 1 - pt.life / pt.max
      if (pt.kind === 'z') pt.el.setAttribute('transform', `translate(${pt.x + Math.sin(pt.life * 3) * 3} ${pt.y}) scale(${0.6 + pt.life * 0.25})`)
      else pt.el.setAttribute('transform', `translate(${pt.x} ${pt.y}) rotate(${pt.life * 200}) scale(${pt.sc})`)
      pt.el.setAttribute('opacity', Math.max(0, Math.min(1, a * 1.6)).toFixed(3))
    }
    this.parts = this.parts.filter((pt) => {
      if (pt.life >= pt.max) {
        pt.el.remove()
        return false
      }
      return true
    })
  }

  private spawn(kind: Particle['kind'], x: number, y: number, vx: number, vy: number, sc = 1): void {
    let el: SVGElement
    if (kind === 'z') {
      el = document.createElementNS(NS, 'text')
      el.textContent = 'z'
      el.setAttribute('fill', C.teal)
      el.setAttribute('font-family', 'Outfit, sans-serif')
      el.setAttribute('font-weight', '700')
      el.setAttribute('font-size', '16')
    } else if (kind === 'crumb') {
      el = document.createElementNS(NS, 'circle')
      el.setAttribute('r', String(1.3 + Math.random()))
      el.setAttribute('fill', C.gold)
    } else {
      el = document.createElementNS(NS, 'path')
      el.setAttribute('d', 'M0 -5 L1.3 -1.3 L5 0 L1.3 1.3 L0 5 L-1.3 1.3 L-5 0 L-1.3 -1.3 Z')
      el.setAttribute('fill', Math.random() < 0.7 ? C.teal : C.gold)
    }
    this.el.fxg.appendChild(el)
    this.parts.push({
      el, kind, x, y, vx, vy,
      sc: sc * (kind === 'spark' ? 0.7 + Math.random() * 0.6 : 1),
      life: 0,
      max: kind === 'z' ? 2.6 : kind === 'crumb' ? 1.5 : 0.9,
    })
  }

  private render(): void {
    const p = this.p
    const e = this.el
    const f = (n: number) => n.toFixed(2)
    e.char.setAttribute('transform', `translate(0 ${f(p.bodyY)}) translate(100 182) rotate(${f(p.lean)}) scale(${f(p.sx)} ${f(p.sy)}) translate(-100 -182)`)
    e.head.setAttribute('transform', `translate(0 ${f(p.headY)}) rotate(${f(p.headRot)} 102 104)`)
    e.earL.setAttribute('transform', `rotate(${f(p.earL + (this.twitch.ear < 0 ? -this.tw : 0))} 85 50)`)
    e.earR.setAttribute('transform', `rotate(${f(p.earR + (this.twitch.ear > 0 ? this.tw : 0))} 119 50)`)
    const lift = Math.max(0, -p.bodyY)
    e.shadow.setAttribute('rx', f(42 * (1 - lift / 70)))
    e.shadow.setAttribute('opacity', f(1 - lift / 45))
    // Eyes.
    const openS = Math.max(0, p.open * (1 - p.happy) * (1 - p.closed) * this.blink)
    const closedO = Math.min(1, Math.max(p.closed, (1 - p.happy) * Math.max(0, (0.45 - openS) * 3)))
    for (const g of e.eyes) {
      const cx = Number(g.getAttribute('data-cx'))
      const cy = Number(g.getAttribute('data-cy'))
      const [eo, eh, ec] = [...g.children]
      eo.setAttribute('transform', `translate(${cx} ${cy}) scale(1 ${f(Math.max(0.02, openS))}) translate(${-cx} ${-cy})`)
      eo.setAttribute('opacity', openS < 0.08 ? '0' : '1')
      eo.querySelector('.iris')?.setAttribute('transform', `translate(${f(p.pupX)} ${f(p.pupY * 0.6)})`)
      eh.setAttribute('opacity', f(p.happy))
      ec.setAttribute('opacity', f(closedO))
    }
    e.brows.setAttribute('opacity', f(p.worry))
    e.mo.setAttribute('transform', `translate(102 94) scale(1 ${f(Math.max(0.01, p.mouth))}) translate(-102 -94)`)
    e.mo.setAttribute('opacity', p.mouth > 0.05 ? '1' : '0')
    e.blush.setAttribute('opacity', f(p.blush))
    e.sweat.setAttribute('opacity', f(p.sweat))
    e.sweat.setAttribute('transform', `translate(0 ${f(Math.sin(this.t * 3) * 1.5)})`)
    // Acorn and arms.
    e.acorn.setAttribute('transform', `translate(${f(p.ax)} ${f(p.ay)}) rotate(${f(p.ar)})`)
    e.glow.setAttribute('opacity', f(p.glow))
    const sh: [number, number, number, number][] = [
      [92, 117, p.pLx, p.pLy],
      [p.sRx, p.sRy, p.pRx, p.pRy],
    ]
    sh.forEach(([sx, sy, px, py], i) => {
      const d = `M${f(sx)} ${f(sy)} L${f(px)} ${f(py)}`
      e.limbs[i].setAttribute('d', d)
      e.limbs[i + 2].setAttribute('d', d)
      e.paws[i].setAttribute('cx', f(px))
      e.paws[i].setAttribute('cy', f(py))
    })
    // Thinking dots and speed lines.
    e.dots.setAttribute('opacity', f(p.dots))
    if (p.dots > 0.01) {
      ;[...e.dots.children].forEach((c, i) => {
        const v = this.still ? 0 : Math.sin(this.t * 6 - i * 0.9)
        c.setAttribute('transform', `translate(0 ${f(-Math.max(0, v) * 4)})`)
        c.setAttribute('opacity', f(0.35 + 0.65 * Math.max(0, v)))
      })
    }
    e.speed.setAttribute('opacity', f(p.speed * 0.8))
    e.speed.setAttribute('stroke-dashoffset', f(this.t * 160))
    // Tail: 18 circles along a jointed chain.
    const N = 18
    const L = 8.6
    let x = 86
    let y = 154
    let a = p.tailA * DEG
    const [out, main, dk, lt] = e.tl
    for (let i = 0; i < N; i++) {
      const fr = i / (N - 1)
      const r = 8 + 14 * Math.sin(Math.PI * (0.08 + 0.82 * fr))
      out[i].setAttribute('cx', f(x))
      out[i].setAttribute('cy', f(y))
      out[i].setAttribute('r', f(r + 2.4))
      main[i].setAttribute('cx', f(x))
      main[i].setAttribute('cy', f(y))
      main[i].setAttribute('r', f(r))
      dk[i].setAttribute('cx', f(x + r * 0.22))
      dk[i].setAttribute('cy', f(y + r * 0.22))
      dk[i].setAttribute('r', f(r * 0.72))
      lt[i].setAttribute('cx', f(x - r * 0.26))
      lt[i].setAttribute('cy', f(y - r * 0.3))
      lt[i].setAttribute('r', f(r * 0.44))
      const sway = p.swayAmp * DEG * Math.sin((this.phase * 2 * Math.PI) / 2 - i * 0.5) * (0.4 + 0.6 * fr)
      a += (p.curl0 + p.curl1 * fr * fr * fr) * DEG + sway
      x += Math.cos(a) * L
      y += Math.sin(a) * L
    }
  }
}
