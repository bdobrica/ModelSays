import { act } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import {
  RoomEventClient,
  roomEventTypes,
  type InvalidationBatch,
  type RoomInvalidation,
  type RoomConnectionState,
} from './roomEvents'

const encoder = new TextEncoder()

function event(revision: number, type: RoomInvalidation['type'] = 'player_joined'): RoomInvalidation {
  return {
    version: 1,
    id: `event-${revision}`,
    roomCode: 'ABC234',
    type,
    roomRevision: revision,
    occurredAt: '2026-07-19T12:00:00Z',
  }
}

function frame(value: RoomInvalidation | Record<string, unknown>, name = 'room_invalidation') {
  return `id: ${String(value.roomRevision)}\nevent: ${name}\ndata: ${JSON.stringify(value)}\n\n`
}

function openStream() {
  let streamController!: ReadableStreamDefaultController<Uint8Array>
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      streamController = controller
    },
  })
  return { body, streamController }
}

async function settle() {
  await act(async () => {
    await Promise.resolve()
    await Promise.resolve()
  })
}

beforeEach(() => {
  vi.useFakeTimers()
  Object.defineProperty(window.navigator, 'onLine', { configurable: true, value: true })
})

afterEach(() => {
  Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' })
  vi.useRealTimers()
})

describe('RoomEventClient', () => {
  it('authenticates without URL credentials and coalesces every content-free event type', async () => {
    const { body, streamController } = openStream()
    const fetchMock = vi.fn<typeof window.fetch>().mockResolvedValue(new Response(body, { status: 200 }))
    const batches: InvalidationBatch[] = []
    const states: RoomConnectionState[] = []
    const client = new RoomEventClient({
      roomCode: 'ABC234',
      playerToken: 'private-token',
      initialRevision: 0,
      fetch: fetchMock,
      coalesceMilliseconds: 10,
      onStateChange: (state) => states.push(state),
      onInvalidations: async (batch) => {
        batches.push(batch)
        return batch.events.at(-1)!.roomRevision
      },
    })

    client.start()
    await settle()
    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringMatching(/\/api\/rooms\/ABC234\/events$/),
      expect.objectContaining({
        headers: expect.objectContaining({
          Accept: 'text/event-stream',
          'Last-Event-ID': '0',
          'X-Player-Token': 'private-token',
        }),
      }),
    )
    expect(fetchMock.mock.calls[0][0]).not.toContain('private-token')
    expect(states).toContain('live')

    streamController.enqueue(encoder.encode(roomEventTypes.map((type, index) => frame(event(index + 1, type))).join('')))
    await settle()
    await act(async () => vi.advanceTimersByTime(10))

    expect(batches).toHaveLength(1)
    expect(batches[0].events.map((item) => item.type)).toEqual(roomEventTypes)
    expect(batches[0].hasRevisionGap).toBe(false)
    const allowedKeys = ['id', 'occurredAt', 'roomCode', 'roomRevision', 'type', 'version']
    for (const item of batches[0].events) {
      expect(Object.keys(item).sort()).toEqual(allowedKeys)
    }
    client.stop()
  })

  it('drops duplicate and stale revisions, sorts bursts, and reports a revision gap', async () => {
    const { body, streamController } = openStream()
    const onInvalidations = vi.fn(async (batch: InvalidationBatch) => batch.events.at(-1)!.roomRevision)
    const client = new RoomEventClient({
      roomCode: 'ABC234',
      playerToken: 'token',
      initialRevision: 4,
      fetch: vi.fn<typeof window.fetch>().mockResolvedValue(new Response(body)),
      coalesceMilliseconds: 1,
      onStateChange: vi.fn(),
      onInvalidations,
    })
    client.start()
    await settle()
    streamController.enqueue(encoder.encode([
      frame(event(7)),
      frame(event(4)),
      frame(event(6)),
      frame(event(6)),
    ].join('')))
    await settle()
    await act(async () => vi.advanceTimersByTime(1))

    expect(onInvalidations).toHaveBeenCalledWith({
      events: [event(6), event(7)],
      hasRevisionGap: true,
    })
    client.stop()
  })

  it('ignores malformed, wrong-room, unknown-version, and non-invalidation frames', async () => {
    const { body, streamController } = openStream()
    const onInvalidations = vi.fn()
    const client = new RoomEventClient({
      roomCode: 'ABC234',
      playerToken: 'token',
      initialRevision: 0,
      fetch: vi.fn<typeof window.fetch>().mockResolvedValue(new Response(body)),
      coalesceMilliseconds: 1,
      onStateChange: vi.fn(),
      onInvalidations,
    })
    client.start()
    await settle()
    streamController.enqueue(encoder.encode([
      'event: room_invalidation\ndata: not-json\n\n',
      frame({ ...event(1), version: 2 }),
      frame({ ...event(2), roomCode: 'OTHER1' }),
      frame(event(3), 'secret_state'),
      frame({ ...event(4), board: { answers: ['secret'] } }),
    ].join('')))
    await settle()
    await act(async () => vi.advanceTimersByTime(1))
    expect(onInvalidations).not.toHaveBeenCalled()
    client.stop()
  })

  it('falls back, reconnects with bounded backoff, and resumes from applied state', async () => {
    const first = openStream()
    const second = openStream()
    const fetchMock = vi.fn<typeof window.fetch>()
      .mockResolvedValueOnce(new Response(first.body))
      .mockResolvedValueOnce(new Response(second.body))
    const states: RoomConnectionState[] = []
    const client = new RoomEventClient({
      roomCode: 'ABC234',
      playerToken: 'token',
      initialRevision: 8,
      fetch: fetchMock,
      minBackoffMilliseconds: 100,
      maxBackoffMilliseconds: 200,
      coalesceMilliseconds: 1,
      onStateChange: (state) => states.push(state),
      onInvalidations: async () => 9,
    })
    client.start()
    await settle()
    first.streamController.enqueue(encoder.encode(frame(event(9))))
    await settle()
    await act(async () => vi.advanceTimersByTime(1))
    first.streamController.close()
    await settle()
    expect(states).toContain('fallback')

    await act(async () => vi.advanceTimersByTime(100))
    await settle()
    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect((fetchMock.mock.calls[1][1]?.headers as Record<string, string>)['Last-Event-ID']).toBe('9')
    client.stop()
  })

  it('reduces hidden-tab work, reconnects on visibility/network recovery, and stops lifecycle work', async () => {
    const streams = [openStream(), openStream(), openStream()]
    const fetchMock = vi.fn<typeof window.fetch>()
      .mockResolvedValueOnce(new Response(streams[0].body))
      .mockResolvedValueOnce(new Response(streams[1].body))
      .mockResolvedValueOnce(new Response(streams[2].body))
    const states: RoomConnectionState[] = []
    const client = new RoomEventClient({
      roomCode: 'ABC234',
      playerToken: 'token',
      initialRevision: 0,
      fetch: fetchMock,
      onStateChange: (state) => states.push(state),
      onInvalidations: async () => 0,
    })
    client.start()
    await settle()
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' })
    document.dispatchEvent(new Event('visibilitychange'))
    await settle()
    expect(fetchMock).toHaveBeenCalledTimes(1)
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' })
    document.dispatchEvent(new Event('visibilitychange'))
    await settle()
    expect(fetchMock).toHaveBeenCalledTimes(2)

    Object.defineProperty(window.navigator, 'onLine', { configurable: true, value: false })
    window.dispatchEvent(new Event('offline'))
    expect(states.at(-1)).toBe('offline')

    Object.defineProperty(window.navigator, 'onLine', { configurable: true, value: true })
    window.dispatchEvent(new Event('online'))
    await settle()
    expect(fetchMock).toHaveBeenCalledTimes(3)
    client.stop()
    expect(states.at(-1)).toBe('stopped')

    window.dispatchEvent(new Event('online'))
    document.dispatchEvent(new Event('visibilitychange'))
    await settle()
    expect(fetchMock).toHaveBeenCalledTimes(3)
  })
})
