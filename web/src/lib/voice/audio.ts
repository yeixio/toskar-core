import { VOICE_RATE, resample } from './pcm'
import { THINKING_DELAY_MS, THINKING_GAP_MS, thinkingSound } from './thinking'

/** Passes the microphone's samples to the page, 128 at a time. */
const captureWorklet = `
class Capture extends AudioWorkletProcessor {
  process(inputs) {
    const ch = inputs[0] && inputs[0][0]
    if (ch) this.port.postMessage(ch.slice(0))
    return true
  }
}
registerProcessor('toskar-capture', Capture)
`

/** Whether this page may use the microphone: it needs a secure page (https or localhost). */
export function canUseMicrophone(): boolean {
  return typeof navigator !== 'undefined' && !!navigator.mediaDevices?.getUserMedia && typeof AudioContext !== 'undefined'
}

/**
 * The microphone and the speaker for live voice: the microphone's audio
 * comes out at VOICE_RATE through onSamples, and play queues the answer's
 * audio to play right after what's already queued.
 */
export class VoiceAudio {
  private context: AudioContext
  private stream: MediaStream | null = null
  private source: MediaStreamAudioSourceNode | null = null
  private node: AudioWorkletNode | null = null
  private playing = new Set<AudioBufferSourceNode>()
  private playAt = 0
  private muted = false
  private thinkingTimer: ReturnType<typeof setTimeout> | null = null
  private chime: AudioBufferSourceNode | null = null
  /** Called when everything queued has played. */
  onDrained?: () => void

  constructor() {
    this.context = new AudioContext()
  }

  /** Asks for the microphone and starts sending its audio. */
  async start(onSamples: (samples: Float32Array) => void) {
    // The browser's echo cancellation keeps the answer, from the speakers,
    // from sounding like the person talking over it.
    this.stream = await navigator.mediaDevices.getUserMedia({
      audio: { echoCancellation: true, noiseSuppression: true, autoGainControl: true, channelCount: 1 },
    })
    await this.context.resume()
    const url = URL.createObjectURL(new Blob([captureWorklet], { type: 'application/javascript' }))
    try {
      await this.context.audioWorklet.addModule(url)
    } finally {
      URL.revokeObjectURL(url)
    }
    this.source = this.context.createMediaStreamSource(this.stream)
    this.node = new AudioWorkletNode(this.context, 'toskar-capture')
    const rate = this.context.sampleRate
    this.node.port.onmessage = (ev: MessageEvent<Float32Array>) => {
      if (this.muted) return
      onSamples(resample(ev.data, rate, VOICE_RATE))
    }
    this.source.connect(this.node)
  }

  /** Mutes or unmutes the microphone: muted, nothing is sent. */
  setMuted(muted: boolean) {
    this.muted = muted
    for (const t of this.stream?.getAudioTracks() ?? []) t.enabled = !muted
  }

  /** Plays samples at VOICE_RATE after what's already queued. */
  play(samples: Float32Array) {
    if (samples.length === 0) return
    const buffer = this.context.createBuffer(1, samples.length, VOICE_RATE)
    buffer.copyToChannel(samples, 0)
    const node = this.context.createBufferSource()
    node.buffer = buffer
    node.connect(this.context.destination)
    const now = this.context.currentTime
    // A little room when starting from silence, so the first piece doesn't clip.
    this.playAt = Math.max(this.playAt, now + 0.05)
    node.start(this.playAt)
    this.playAt += buffer.duration
    this.playing.add(node)
    node.onended = () => {
      this.playing.delete(node)
      if (this.playing.size === 0) this.onDrained?.()
    }
  }

  /**
   * Starts the thinking sound (#1334): after a moment without an answer, a
   * soft chime, again every few seconds, until stopThinking. It isn't the
   * answer, so it doesn't count as playing.
   */
  startThinking() {
    if (this.thinkingTimer) return
    const chime = () => {
      const samples = thinkingSound(this.context.sampleRate)
      const buffer = this.context.createBuffer(1, samples.length, this.context.sampleRate)
      buffer.copyToChannel(samples, 0)
      const node = this.context.createBufferSource()
      node.buffer = buffer
      node.connect(this.context.destination)
      node.start()
      this.chime = node
      node.onended = () => {
        if (this.chime === node) this.chime = null
      }
      this.thinkingTimer = setTimeout(chime, THINKING_GAP_MS)
    }
    this.thinkingTimer = setTimeout(chime, THINKING_DELAY_MS)
  }

  /** Stops the thinking sound at once, before the answer plays. */
  stopThinking() {
    if (this.thinkingTimer) clearTimeout(this.thinkingTimer)
    this.thinkingTimer = null
    if (this.chime) {
      this.chime.onended = null
      try {
        this.chime.stop()
      } catch {
        // already ended
      }
      this.chime = null
    }
  }

  /** Stops the answer at once, when the person talks over it. */
  interrupt() {
    for (const node of this.playing) {
      node.onended = null
      try {
        node.stop()
      } catch {
        // not started yet
      }
    }
    this.playing.clear()
    this.playAt = 0
  }

  get isPlaying() {
    return this.playing.size > 0
  }

  close() {
    this.stopThinking()
    this.interrupt()
    this.node?.disconnect()
    this.source?.disconnect()
    for (const t of this.stream?.getTracks() ?? []) t.stop()
    void this.context.close()
  }
}
