import { describe, expect, it, vi } from 'vitest'
import { VOICE_RATE, fromBase64, fromPCM16, resample, toBase64, toPCM16 } from './pcm'
import { VoiceConnection, resolveVoiceSocketUrl, voiceProtocols, voiceSocketUrl, type SocketLike, type VoiceStatus } from './realtime'

describe('pcm', () => {
  it('round-trips 16-bit PCM and base64', () => {
    const samples = new Float32Array([0, 0.5, -0.5, 1, -1])
    const back = fromPCM16(fromBase64(toBase64(toPCM16(samples))))
    for (let i = 0; i < samples.length; i++) expect(back[i]).toBeCloseTo(samples[i], 3)
  })

  it('resamples a microphone to the voice rate', () => {
    const mic = new Float32Array(48_000).map((_, i) => Math.sin(i / 10))
    expect(resample(mic, 48_000, VOICE_RATE).length).toBe(24_000)
    expect(resample(mic, VOICE_RATE, VOICE_RATE)).toBe(mic)
  })
})

class FakeSocket implements SocketLike {
  readyState = 0
  onopen: ((ev: Event) => void) | null = null
  onmessage: ((ev: MessageEvent) => void) | null = null
  onclose: ((ev: CloseEvent) => void) | null = null
  onerror: ((ev: Event) => void) | null = null
  sent: Record<string, unknown>[] = []
  constructor(
    public url: string,
    public protocols: string[],
  ) {}
  send(data: string) {
    this.sent.push(JSON.parse(data) as Record<string, unknown>)
  }
  close() {
    this.readyState = 3
    this.onclose?.({} as CloseEvent)
  }
  open() {
    this.readyState = 1
    this.receive({ type: 'session.created' })
  }
  receive(e: Record<string, unknown>) {
    this.onmessage?.({ data: JSON.stringify(e) } as MessageEvent)
  }
}

function connect() {
  let socket!: FakeSocket
  const statuses: VoiceStatus[] = []
  const handlers = {
    onStatus: (s: VoiceStatus) => statuses.push(s),
    onYou: vi.fn(),
    onAnswer: vi.fn(),
    onAudio: vi.fn(),
    onInterrupt: vi.fn(),
    onDone: vi.fn(),
    onError: vi.fn(),
  }
  const conn = new VoiceConnection(handlers, (url, protocols) => (socket = new FakeSocket(url, protocols)))
  return { conn, socket, statuses, handlers }
}

describe('VoiceConnection', () => {
  it('connects to the realtime address, with the key as a subprotocol when there is one', () => {
    expect(voiceSocketUrl()).toBe(window.location.origin.replace(/^http/, 'ws') + '/api/v1/voice/realtime')
    expect(voiceProtocols('')).toEqual(['realtime'])
    expect(voiceProtocols('tsk_1')).toEqual(['realtime', 'openai-insecure-api-key.tsk_1'])
    const { socket } = connect()
    expect(socket.url).toMatch(/^wss?:\/\/.+\/api\/v1\/voice\/realtime$/)
    expect(socket.protocols[0]).toBe('realtime')
  })

  it("uses the daemon's own address in the desktop app, where the proxy can't carry a WebSocket", async () => {
    const w = window as unknown as { go?: unknown }
    w.go = { main: { App: { VoiceSocketURL: () => Promise.resolve('ws://127.0.0.1:7331/api/v1/voice/realtime') } } }
    try {
      expect(await resolveVoiceSocketUrl()).toBe('ws://127.0.0.1:7331/api/v1/voice/realtime')
      // An older shell without it: this page's address.
      w.go = { main: { App: {} } }
      expect(await resolveVoiceSocketUrl()).toBe(voiceSocketUrl())
    } finally {
      delete w.go
    }
    expect(await resolveVoiceSocketUrl()).toBe(voiceSocketUrl())
    let opened = ''
    new VoiceConnection({}, (url) => {
      opened = url
      return new FakeSocket(url, [])
    }, 'ws://127.0.0.1:7331/api/v1/voice/realtime')
    expect(opened).toBe('ws://127.0.0.1:7331/api/v1/voice/realtime')
  })

  it('sends audio in 100 ms pieces once connected', () => {
    const { conn, socket } = connect()
    conn.send(new Float32Array(2400))
    expect(socket.sent).toHaveLength(0)
    socket.open()
    conn.send(new Float32Array(1200))
    expect(socket.sent).toHaveLength(0)
    conn.send(new Float32Array(1200))
    expect(socket.sent).toHaveLength(1)
    expect(socket.sent[0].type).toBe('input_audio_buffer.append')
    expect(fromBase64(String(socket.sent[0].audio)).length).toBe(2400 * 2)
  })

  it('follows a turn: hearing, thinking, speaking, and listening once played', () => {
    const { conn, socket, statuses, handlers } = connect()
    socket.open()
    socket.receive({ type: 'input_audio_buffer.speech_started' })
    socket.receive({ type: 'input_audio_buffer.committed' })
    socket.receive({ type: 'conversation.item.input_audio_transcription.completed', transcript: ' Hi there ' })
    socket.receive({ type: 'response.created' })
    socket.receive({ type: 'response.audio_transcript.delta', delta: 'Hello! ' })
    socket.receive({ type: 'response.audio.delta', delta: toBase64(toPCM16(new Float32Array(2400))) })
    socket.receive({ type: 'response.done', response: { status: 'completed', metadata: { toskar_latency: { end_to_end_ms: 900 } } } })
    conn.played()
    expect(statuses).toEqual(['listening', 'hearing', 'thinking', 'speaking', 'listening'])
    expect(handlers.onYou).toHaveBeenCalledWith('Hi there')
    expect(handlers.onAnswer).toHaveBeenLastCalledWith('Hello!')
    expect(handlers.onAudio.mock.calls[0][0]).toHaveLength(2400)
    expect(handlers.onDone).toHaveBeenCalledWith('completed', 900)
    expect(handlers.onInterrupt).not.toHaveBeenCalled()
  })

  it('keeps the captions up until the next words replace them', () => {
    const { socket, handlers } = connect()
    socket.open()
    socket.receive({ type: 'conversation.item.input_audio_transcription.completed', transcript: 'Hi there' })
    socket.receive({ type: 'response.created' })
    socket.receive({ type: 'response.audio_transcript.delta', delta: 'Hello! ' })
    socket.receive({ type: 'response.done', response: { status: 'completed' } })
    // A noise heard as nothing, and the next answer starting.
    socket.receive({ type: 'conversation.item.input_audio_transcription.completed', transcript: '' })
    socket.receive({ type: 'response.created' })
    expect(handlers.onYou).toHaveBeenLastCalledWith('Hi there')
    expect(handlers.onAnswer).toHaveBeenLastCalledWith('Hello!')
    socket.receive({ type: 'response.audio_transcript.delta', delta: 'Sure. ' })
    expect(handlers.onAnswer).toHaveBeenLastCalledWith('Sure.')
  })

  it('stops the answer when the person talks over it', () => {
    const { socket, statuses, handlers } = connect()
    socket.open()
    socket.receive({ type: 'input_audio_buffer.committed' })
    socket.receive({ type: 'response.audio.delta', delta: toBase64(toPCM16(new Float32Array(240))) })
    socket.receive({ type: 'input_audio_buffer.speech_started' })
    expect(handlers.onInterrupt).toHaveBeenCalledOnce()
    expect(statuses.at(-1)).toBe('hearing')
  })

  it('listens again when nothing was heard or hearing failed', () => {
    const { socket, statuses, handlers } = connect()
    socket.open()
    socket.receive({ type: 'input_audio_buffer.committed' })
    socket.receive({ type: 'conversation.item.input_audio_transcription.completed', transcript: '' })
    expect(statuses.at(-1)).toBe('listening')
    socket.receive({ type: 'input_audio_buffer.committed' })
    socket.receive({ type: 'conversation.item.input_audio_transcription.failed', error: { message: 'no whisper' } })
    expect(statuses.at(-1)).toBe('listening')
    expect(handlers.onError).toHaveBeenCalledWith('VOICE_HEARING_FAILED', 'no whisper')
  })

  it('says when it could not connect', () => {
    const { socket, statuses, handlers } = connect()
    socket.close()
    expect(statuses).toEqual(['failed'])
    expect(handlers.onError).toHaveBeenCalledWith('VOICE_CONNECT_FAILED', 'could not connect')
  })
})
