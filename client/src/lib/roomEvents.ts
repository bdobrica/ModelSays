import { env } from './env'

export const roomEventTypes = [
  'player_joined',
  'game_started',
  'submission_progress_changed',
  'round_revealed',
  'score_changed',
  'round_started',
  'game_completed',
] as const

export type RoomEventType = (typeof roomEventTypes)[number]
export type RoomConnectionState = 'connecting' | 'live' | 'fallback' | 'offline' | 'stopped'

export interface RoomInvalidation {
  version: 1
  id: string
  roomCode: string
  type: RoomEventType
  roomRevision: number
  occurredAt: string
}

export interface InvalidationBatch {
  events: RoomInvalidation[]
  hasRevisionGap: boolean
}

interface RoomEventClientOptions {
  roomCode: string
  playerToken: string
  initialRevision: number
  onConnected?: () => Promise<number>
  onInvalidations: (batch: InvalidationBatch) => Promise<number>
  onStateChange: (state: RoomConnectionState) => void
  fetch?: typeof window.fetch
  minBackoffMilliseconds?: number
  maxBackoffMilliseconds?: number
  coalesceMilliseconds?: number
}

const eventTypeSet = new Set<string>(roomEventTypes)

export class RoomEventClient {
  private readonly options: Required<Omit<RoomEventClientOptions, 'fetch'>> & { fetch: typeof window.fetch }
  private controller: AbortController | null = null
  private retryTimer: number | null = null
  private coalesceTimer: number | null = null
  private queuedEvents: RoomInvalidation[] = []
  private lastAppliedRevision: number
  private retryCount = 0
  private running = false

  constructor(options: RoomEventClientOptions) {
    this.options = {
      ...options,
      onConnected: options.onConnected ?? (async () => options.initialRevision),
      fetch: options.fetch ?? window.fetch.bind(window),
      minBackoffMilliseconds: options.minBackoffMilliseconds ?? 500,
      maxBackoffMilliseconds: options.maxBackoffMilliseconds ?? 15_000,
      coalesceMilliseconds: options.coalesceMilliseconds ?? 25,
    }
    this.lastAppliedRevision = options.initialRevision
  }

  start() {
    if (this.running) return
    this.running = true
    window.addEventListener('online', this.handleOnline)
    window.addEventListener('offline', this.handleOffline)
    document.addEventListener('visibilitychange', this.handleVisibility)
    if (!navigator.onLine) {
      this.options.onStateChange('offline')
      return
    }
    void this.connect()
  }

  stop() {
    if (!this.running) return
    this.running = false
    this.controller?.abort()
    this.controller = null
    if (this.retryTimer != null) window.clearTimeout(this.retryTimer)
    if (this.coalesceTimer != null) window.clearTimeout(this.coalesceTimer)
    this.retryTimer = null
    this.coalesceTimer = null
    this.queuedEvents = []
    window.removeEventListener('online', this.handleOnline)
    window.removeEventListener('offline', this.handleOffline)
    document.removeEventListener('visibilitychange', this.handleVisibility)
    this.options.onStateChange('stopped')
  }

  setAppliedRevision(revision: number) {
    this.lastAppliedRevision = Math.max(this.lastAppliedRevision, revision)
  }

  private readonly handleOnline = () => {
    if (!this.running) return
    this.retryCount = 0
    this.reconnectNow()
  }

  private readonly handleOffline = () => {
    this.controller?.abort()
    this.options.onStateChange('offline')
  }

  private readonly handleVisibility = () => {
    if (!this.running || document.visibilityState !== 'visible' || !navigator.onLine) return
    this.retryCount = 0
    this.reconnectNow()
  }

  private reconnectNow() {
    this.controller?.abort()
    this.controller = null
    if (this.retryTimer != null) window.clearTimeout(this.retryTimer)
    this.retryTimer = null
    void this.connect()
  }

  private async connect() {
    if (!this.running || !navigator.onLine || this.controller) return
    this.options.onStateChange('connecting')
    const controller = new AbortController()
    this.controller = controller

    try {
      const response = await this.options.fetch(
        `${env.apiBaseUrl}/api/rooms/${encodeURIComponent(this.options.roomCode)}/events`,
        {
          headers: {
            Accept: 'text/event-stream',
            'Last-Event-ID': String(this.lastAppliedRevision),
            'X-Player-Token': this.options.playerToken,
          },
          signal: controller.signal,
        },
      )
      if (!response.ok || !response.body) throw new Error(`event stream unavailable (${response.status})`)
      this.retryCount = 0
      this.options.onStateChange('live')
      const connectedRevision = await this.options.onConnected()
      this.lastAppliedRevision = Math.max(this.lastAppliedRevision, connectedRevision)
      await this.consume(response.body, controller.signal)
      if (!controller.signal.aborted) throw new Error('event stream closed')
    } catch {
      if (!controller.signal.aborted && this.running) this.scheduleReconnect()
    } finally {
      if (this.controller === controller) this.controller = null
    }
  }

  private scheduleReconnect() {
    if (!this.running || !navigator.onLine) return
    this.options.onStateChange('fallback')
    const delay = Math.min(
      this.options.maxBackoffMilliseconds,
      this.options.minBackoffMilliseconds * (2 ** this.retryCount),
    )
    this.retryCount += 1
    this.retryTimer = window.setTimeout(() => {
      this.retryTimer = null
      void this.connect()
    }, delay)
  }

  private async consume(stream: ReadableStream<Uint8Array>, signal: AbortSignal) {
    const reader = stream.getReader()
    const decoder = new TextDecoder()
    let buffer = ''
    try {
      while (!signal.aborted) {
        const { done, value } = await reader.read()
        if (done) break
        buffer += decoder.decode(value, { stream: true }).replace(/\r\n/g, '\n')
        let boundary = buffer.indexOf('\n\n')
        while (boundary >= 0) {
          this.parseFrame(buffer.slice(0, boundary))
          buffer = buffer.slice(boundary + 2)
          boundary = buffer.indexOf('\n\n')
        }
      }
    } finally {
      reader.releaseLock()
    }
  }

  private parseFrame(frame: string) {
    let eventName = ''
    let data = ''
    for (const line of frame.split('\n')) {
      if (line.startsWith('event:')) eventName = line.slice(6).trim()
      if (line.startsWith('data:')) data += line.slice(5).trim()
    }
    if (eventName !== 'room_invalidation' || !data) return

    try {
      const candidate = JSON.parse(data) as Partial<RoomInvalidation>
      const keys = Object.keys(candidate).sort()
      if (
        keys.join(',') !== 'id,occurredAt,roomCode,roomRevision,type,version' ||
        candidate.version !== 1 ||
        typeof candidate.id !== 'string' ||
        candidate.roomCode !== this.options.roomCode ||
        typeof candidate.type !== 'string' ||
        !eventTypeSet.has(candidate.type) ||
        !Number.isSafeInteger(candidate.roomRevision) ||
        (candidate.roomRevision ?? 0) < 0 ||
        typeof candidate.occurredAt !== 'string'
      ) return
      this.queuedEvents.push(candidate as RoomInvalidation)
      if (this.coalesceTimer == null) {
        this.coalesceTimer = window.setTimeout(() => void this.flush(), this.options.coalesceMilliseconds)
      }
    } catch {
      // Ignore malformed or future-version frames; fallback polling remains authoritative.
    }
  }

  private async flush() {
    this.coalesceTimer = null
    if (!this.running || this.queuedEvents.length === 0) return
    const queued = this.queuedEvents
    this.queuedEvents = []
    const unique = [...new Map(
      queued
        .filter((event) => event.roomRevision > this.lastAppliedRevision)
        .map((event) => [event.roomRevision, event]),
    ).values()].sort((left, right) => left.roomRevision - right.roomRevision)
    if (unique.length === 0) return

    let expected = this.lastAppliedRevision + 1
    let hasRevisionGap = false
    for (const event of unique) {
      if (event.roomRevision !== expected) hasRevisionGap = true
      expected = event.roomRevision + 1
    }
    try {
      const appliedRevision = await this.options.onInvalidations({ events: unique, hasRevisionGap })
      this.lastAppliedRevision = Math.max(this.lastAppliedRevision, appliedRevision)
    } catch {
      // Do not advance the resume cursor unless authoritative room state was applied.
    }
    if (this.queuedEvents.length > 0 && this.coalesceTimer == null) {
      this.coalesceTimer = window.setTimeout(() => void this.flush(), this.options.coalesceMilliseconds)
    }
  }
}
