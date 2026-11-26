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

/** How voice mode uses the microphone, kept on this device (#1330). */
export interface MicrophoneChoice {
  /** The input's deviceId, or '' for the system's default. */
  deviceId: string
  /** The browser evens out the level: helps a quiet voice, but can raise a room's hiss between words. */
  autoGain: boolean
}

const micKey = 'toskar.voice.microphone'

/** The microphone chosen on this device, or the default with auto gain on. */
export function readMicrophoneChoice(): MicrophoneChoice {
  try {
    const saved = JSON.parse(window.localStorage.getItem(micKey) ?? '{}') as Partial<MicrophoneChoice>
    return { deviceId: typeof saved.deviceId === 'string' ? saved.deviceId : '', autoGain: saved.autoGain !== false }
  } catch {
    return { deviceId: '', autoGain: true }
  }
}

export function saveMicrophoneChoice(choice: MicrophoneChoice) {
  try {
    window.localStorage.setItem(micKey, JSON.stringify(choice))
  } catch {
    // storage blocked: the choice lasts this conversation
  }
}

/** The microphones this browser can use, once it has been allowed one. */
export async function listMicrophones(): Promise<{ deviceId: string; label: string }[]> {
  if (!navigator.mediaDevices?.enumerateDevices) return []
  const devices = await navigator.mediaDevices.enumerateDevices()
  return devices
    .filter((d) => d.kind === 'audioinput' && d.deviceId && d.deviceId !== 'default' && d.deviceId !== 'communications')
    .map((d) => ({ deviceId: d.deviceId, label: d.label }))
}

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
  /** The answer passes through here on its way out, so an avatar can follow how loud it is (#1369). */
  private output: AnalyserNode
  private samples: Float32Array<ArrayBuffer>
  /** Called when everything queued has played. */
  onDrained?: () => void

  constructor() {
    this.context = new AudioContext()
    this.output = this.context.createAnalyser()
    this.output.fftSize = 512
    this.output.connect(this.context.destination)
    this.samples = new Float32Array(this.output.fftSize)
  }

  /**
   * How loud the answer is right now, 0 to 1, for an avatar's mouth: the
   * level of the last few milliseconds that played. The thinking sound
   * doesn't count.
   */
  level(): number {
    if (this.playing.size === 0) return 0
    this.output.getFloatTimeDomainData(this.samples)
    let sum = 0
    for (const v of this.samples) sum += v * v
    return Math.min(1, Math.sqrt(sum / this.samples.length) * 5)
  }

  /** Asks for the microphone and starts sending its audio. */
  async start(onSamples: (samples: Float32Array) => void, choice: MicrophoneChoice = { deviceId: '', autoGain: true }) {
    this.stream = await openMicrophone(choice)
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

  /** The microphone in use, as the browser names it. */
  get microphone(): { deviceId: string; label: string } {
    const track = this.stream?.getAudioTracks()[0]
    return { deviceId: track?.getSettings().deviceId ?? '', label: track?.label ?? '' }
  }

  /** Changes the microphone, or its auto gain, without ending the conversation. */
  async useMicrophone(choice: MicrophoneChoice) {
    if (!this.node) return
    const stream = await openMicrophone(choice)
    this.source?.disconnect()
    for (const t of this.stream?.getTracks() ?? []) t.stop()
    this.stream = stream
    for (const t of stream.getAudioTracks()) t.enabled = !this.muted
    this.source = this.context.createMediaStreamSource(stream)
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
    node.connect(this.output)
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

/**
 * Opens the microphone. The browser's echo cancellation keeps the answer,
 * from the speakers, from sounding like the person talking over it. A
 * chosen microphone that's gone falls back to the default.
 */
async function openMicrophone(choice: MicrophoneChoice): Promise<MediaStream> {
  const audio: MediaTrackConstraints = { echoCancellation: true, noiseSuppression: true, autoGainControl: choice.autoGain, channelCount: 1 }
  if (choice.deviceId) {
    try {
      return await navigator.mediaDevices.getUserMedia({ audio: { ...audio, deviceId: { exact: choice.deviceId } } })
    } catch (err) {
      if (!(err instanceof DOMException) || (err.name !== 'OverconstrainedError' && err.name !== 'NotFoundError')) throw err
    }
  }
  return navigator.mediaDevices.getUserMedia({ audio })
}
