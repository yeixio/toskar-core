import { getApiBase, storedApiKey } from '@/lib/api'
import { desktopVoiceSocketUrl } from '@/lib/desktopBridge'
import { VOICE_RATE, fromBase64, fromPCM16, toBase64, toPCM16 } from './pcm'

/** Where a live voice conversation is, as the person sees it. */
export type VoiceStatus = 'connecting' | 'listening' | 'hearing' | 'thinking' | 'speaking' | 'closed' | 'failed'

export interface VoiceHandlers {
  onStatus?: (status: VoiceStatus) => void
  /** What the person said in a turn, and the language heard, when speech to text was sure of it (#1405). */
  onYou?: (text: string, language?: string) => void
  /** The answer's words so far, as they're spoken. */
  onAnswer?: (text: string) => void
  /** A piece of the answer's audio, at VOICE_RATE. */
  onAudio?: (samples: Float32Array) => void
  /** The person talked over the answer: stop playing it. */
  onInterrupt?: () => void
  /** An answer ended; endToEndMs is how long the person waited for its first audio. */
  onDone?: (status: string, endToEndMs?: number) => void
  onError?: (code: string, message: string) => void
}

/** The parts of a WebSocket the connection uses, so tests can stand in. */
export interface SocketLike {
  readyState: number
  onopen: ((ev: Event) => void) | null
  onmessage: ((ev: MessageEvent) => void) | null
  onclose: ((ev: CloseEvent) => void) | null
  onerror: ((ev: Event) => void) | null
  send(data: string): void
  close(): void
}

/** The live voice address for this page, as ws: or wss:. */
export function voiceSocketUrl(): string {
  const base = getApiBase() || window.location.origin
  return base.replace(/^http/, 'ws').replace(/\/$/, '') + '/api/v1/voice/realtime'
}

/** The live voice address: the daemon's own in the desktop app, else this page's. */
export async function resolveVoiceSocketUrl(): Promise<string> {
  return (await desktopVoiceSocketUrl()) || voiceSocketUrl()
}

/**
 * The subprotocols to ask for: realtime, and the API key when the page has
 * one, since a browser can't set headers on a WebSocket. Without a key, the
 * sign-in cookie goes with the request.
 */
export function voiceProtocols(key = storedApiKey()): string[] {
  return key ? ['realtime', `openai-insecure-api-key.${key}`] : ['realtime']
}

/** About 100 ms of audio per message. */
const sendSamples = VOICE_RATE / 10

/**
 * One live voice conversation over Core's Realtime WebSocket (#795): the
 * microphone's audio goes up, and the answer's words and audio come down.
 * Core hears when the person stops talking and when they talk over an
 * answer.
 */
export class VoiceConnection {
  private socket: SocketLike
  private status: VoiceStatus = 'connecting'
  private answer = ''
  private pending: Float32Array[] = []
  private pendingLength = 0
  private closedByUs = false

  constructor(
    private handlers: VoiceHandlers,
    open: (url: string, protocols: string[]) => SocketLike = (url, protocols) => new WebSocket(url, protocols),
    url = voiceSocketUrl(),
  ) {
    this.socket = open(url, voiceProtocols())
    this.socket.onmessage = (ev) => this.receive(String(ev.data))
    this.socket.onclose = () => {
      if (this.status === 'failed') return
      if (!this.closedByUs && this.status === 'connecting') {
        this.handlers.onError?.('VOICE_CONNECT_FAILED', 'could not connect')
        this.set('failed')
        return
      }
      this.set('closed')
    }
    this.socket.onerror = () => {}
  }

  /** Sends the microphone's audio, at VOICE_RATE. */
  send(samples: Float32Array) {
    if (this.status === 'connecting' || this.status === 'closed' || this.status === 'failed') return
    this.pending.push(samples)
    this.pendingLength += samples.length
    if (this.pendingLength < sendSamples) return
    const all = new Float32Array(this.pendingLength)
    let at = 0
    for (const p of this.pending) {
      all.set(p, at)
      at += p.length
    }
    this.pending = []
    this.pendingLength = 0
    this.event({ type: 'input_audio_buffer.append', audio: toBase64(toPCM16(all)) })
  }

  /** The answer finished playing: listen again. */
  played() {
    if (this.status === 'speaking') this.set('listening')
  }

  close() {
    this.closedByUs = true
    try {
      this.socket.close()
    } catch {
      // already closed
    }
    this.set('closed')
  }

  private event(e: Record<string, unknown>) {
    if (this.socket.readyState === 1) this.socket.send(JSON.stringify(e))
  }

  private set(status: VoiceStatus) {
    if (this.status === status) return
    this.status = status
    this.handlers.onStatus?.(status)
  }

  private receive(data: string) {
    let e: Record<string, unknown>
    try {
      e = JSON.parse(data) as Record<string, unknown>
    } catch {
      return
    }
    switch (e.type) {
      case 'session.created':
        this.set('listening')
        break
      case 'input_audio_buffer.speech_started':
        if (this.status === 'speaking' || this.status === 'thinking') this.handlers.onInterrupt?.()
        this.set('hearing')
        break
      case 'input_audio_buffer.committed':
        this.set('thinking')
        break
      case 'conversation.item.input_audio_transcription.completed': {
        const text = String(e.transcript ?? '').trim()
        // Nothing heard leaves what they said last on screen.
        const language = typeof e.language === 'string' ? e.language : ''
        if (text && language) this.handlers.onYou?.(text, language)
        else if (text) this.handlers.onYou?.(text)
        // Nothing was said, so nothing is answered.
        if (!text && this.status === 'thinking') this.set('listening')
        break
      }
      case 'conversation.item.input_audio_transcription.failed': {
        const err = (e.error ?? {}) as { message?: string }
        this.handlers.onError?.('VOICE_HEARING_FAILED', err.message ?? '')
        if (this.status === 'thinking') this.set('listening')
        break
      }
      case 'response.created':
        // The last answer stays on screen until the next one starts.
        this.answer = ''
        break
      case 'response.audio_transcript.delta':
        this.answer += String(e.delta ?? '')
        this.handlers.onAnswer?.(this.answer.trim())
        break
      case 'response.audio.delta':
        if (this.status !== 'hearing') this.set('speaking')
        this.handlers.onAudio?.(fromPCM16(fromBase64(String(e.delta ?? ''))))
        break
      case 'response.done': {
        const response = (e.response ?? {}) as { status?: string; metadata?: { toskar_latency?: { end_to_end_ms?: number } } }
        this.handlers.onDone?.(response.status ?? 'completed', response.metadata?.toskar_latency?.end_to_end_ms)
        // A text-only or failed answer has nothing left to play.
        if (this.status === 'thinking') this.set('listening')
        break
      }
      case 'error': {
        const err = (e.error ?? {}) as { code?: string; message?: string }
        this.handlers.onError?.(err.code ?? 'VOICE_FAILED', err.message ?? '')
        if (this.status === 'thinking') this.set('listening')
        break
      }
    }
  }
}
