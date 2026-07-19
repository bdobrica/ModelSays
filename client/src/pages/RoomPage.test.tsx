import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import * as api from '../lib/api'
import { saveSession } from '../lib/session'
import { RoomPage } from './RoomPage'

vi.mock('../lib/api', async () => {
  const actual = await vi.importActual<typeof import('../lib/api')>('../lib/api')
  return {
    ...actual,
    getRoom: vi.fn(),
    recoverSession: vi.fn(),
    startGame: vi.fn(),
    submitGuess: vi.fn(),
    revealRound: vi.fn(),
    nextRound: vi.fn(),
    playAgain: vi.fn(),
    overrideMatch: vi.fn(),
    getJudgeSuggestions: vi.fn(),
  }
})

const host = {
  id: 'host-1',
  displayName: 'Host',
  isHost: true,
  joinedAt: '2026-07-19T12:00:00Z',
  token: 'host-token',
}
const player = {
  id: 'player-1',
  displayName: 'Player',
  isHost: false,
  joinedAt: '2026-07-19T12:00:01Z',
  token: 'player-token',
}
const settings = {
  mode: 'simultaneous' as const,
  totalRounds: 2,
  answerTimerSeconds: 30,
  locale: 'en',
  predictionModel: 'gpt-4.1-mini',
  teamSafeMode: false,
}
const question = {
  id: 'question-1',
  text: 'Name a fruit',
  locale: 'en',
  category: 'general',
  createdAt: '2026-07-19T12:00:00Z',
}
const board = {
  id: 'board-1',
  provider: 'static',
  modelName: 'static',
  promptVersion: 'v1',
  boardHash: 'hash',
  createdAt: '2026-07-19T12:00:00Z',
  answers: [{ id: 'answer-1', canonicalAnswer: 'Apple', aliases: [], rank: 1, score: 50, createdAt: '2026-07-19T12:00:00Z' }],
}

function roomState(phase: 'lobby' | 'answering' | 'revealed' | 'completed', submitted = false): api.Room {
  const room: api.Room = {
    code: 'ABC234',
    revision: phase === 'lobby' ? 0 : phase === 'answering' ? 1 : phase === 'revealed' ? 2 : 3,
    name: 'Test room',
    status: phase === 'lobby' ? 'lobby' : phase === 'completed' ? 'completed' : 'in_progress',
    settings,
    players: [host, player],
    createdAt: '2026-07-19T12:00:00Z',
    updatedAt: '2026-07-19T12:00:00Z',
  }
  if (phase === 'lobby') return room
  room.currentGame = {
    id: 'game-1',
    replayId: phase === 'completed' ? 'replay-12345678901234567890123456789012' : undefined,
    status: phase === 'completed' ? 'completed' : 'in_progress',
    mode: 'simultaneous',
    totalRounds: 2,
    currentRoundIndex: phase === 'completed' ? 2 : 1,
    createdAt: '2026-07-19T12:00:00Z',
    startedAt: '2026-07-19T12:00:00Z',
    scoreboard: [
      { playerId: host.id, displayName: host.displayName, isHost: true, score: phase === 'completed' ? 50 : 0, submissionMade: submitted },
      { playerId: player.id, displayName: player.displayName, isHost: false, score: phase === 'completed' ? 50 : 0, submissionMade: false },
    ],
    currentRound: {
      id: 'round-1',
      roundIndex: 1,
      status: phase === 'answering' ? 'answering' : 'revealed',
      question,
      boardHash: 'hash',
      board: phase === 'answering' ? undefined : board,
      guesses: phase === 'answering' ? undefined : [{
        id: 'guess-1',
        playerId: host.id,
        playerDisplayName: host.displayName,
        rawAnswer: 'Apple',
        normalizedAnswer: 'apple',
        matchedPredictionAnswerId: 'answer-1',
        scoreAwarded: 50,
        duplicate: false,
        createdAt: '2026-07-19T12:00:10Z',
      }],
      answerPhaseStartedAt: new Date(Date.now()).toISOString(),
      answerPhaseEndsAt: new Date(Date.now() + 30_000).toISOString(),
      createdAt: '2026-07-19T12:00:00Z',
    },
  }
  return room
}

function renderRoom(code = 'abc234') {
  return render(
    <MemoryRouter initialEntries={[`/room/${code}`]}>
      <Routes>
        <Route path="/room/:code" element={<RoomPage />} />
        <Route path="/" element={<p>Home</p>} />
      </Routes>
    </MemoryRouter>,
  )
}

beforeEach(() => {
  window.localStorage.clear()
  vi.resetAllMocks()
  vi.mocked(api.getJudgeSuggestions).mockResolvedValue({ suggestions: [] })
})

afterEach(() => {
  vi.useRealTimers()
})

describe('RoomPage', () => {
  it('recovers a normalized host session and drives the host game flow with authoritative refetches', async () => {
    saveSession('ABC234', host)
    let current = roomState('lobby')
    vi.mocked(api.recoverSession).mockResolvedValue({ room: current, player: host })
    vi.mocked(api.getRoom).mockImplementation(async () => ({ room: current }))
    vi.mocked(api.startGame).mockImplementation(async () => {
      current = roomState('answering')
      return { room: current }
    })
    vi.mocked(api.submitGuess).mockImplementation(async () => {
      current = roomState('answering', true)
      return { room: current }
    })
    vi.mocked(api.revealRound).mockImplementation(async () => {
      current = roomState('revealed', true)
      return { room: current }
    })
    vi.mocked(api.overrideMatch).mockResolvedValue({ room: current })
    vi.mocked(api.getJudgeSuggestions).mockResolvedValue({ suggestions: [{
      id: 'suggestion-1',
      guessId: 'guess-1',
      suggestedPredictionAnswerId: 'answer-1',
      confidence: 0.91,
      confidenceBand: 'high',
      rationaleCategory: 'paraphrase',
      model: 'gpt-4.1-mini',
      promptVersion: 'judge-v1',
      outcome: 'suggestion',
    }] })
    vi.mocked(api.nextRound).mockImplementation(async () => {
      current = roomState('completed', true)
      return { room: current }
    })

    renderRoom()
    expect(await screen.findByRole('button', { name: 'Start game' })).toBeEnabled()
    expect(api.recoverSession).toHaveBeenCalledWith('ABC234', { playerToken: host.token }, expect.any(AbortSignal))

    fireEvent.click(screen.getByRole('button', { name: 'Start game' }))
    expect(await screen.findByText('30s remaining')).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('Your guess'), { target: { value: 'Apple' } })
    fireEvent.submit(screen.getByLabelText('Your guess').closest('form')!)
    expect(await screen.findByText(/Submitted\. Your guess is locked/)).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Reveal round' }))
    expect(await screen.findByText('Answers and awarded scores are now revealed.')).toBeInTheDocument()
    expect(await screen.findByText(/Judge suggests Apple \(high, 91%; paraphrase\)/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Apply override' }))
    await waitFor(() => expect(api.overrideMatch).toHaveBeenCalled())
    expect(api.overrideMatch).toHaveBeenCalledWith('ABC234', expect.objectContaining({ judgeSuggestionId: 'suggestion-1' }))
    fireEvent.click(screen.getByRole('button', { name: 'Next round' }))

    expect(await screen.findByText('Host — tied winner')).toBeInTheDocument()
    expect(screen.getByText('Player — tied winner')).toBeInTheDocument()
    expect(document.querySelectorAll('.score-rank')).toHaveLength(2)
    expect(vi.mocked(api.getRoom).mock.calls.length).toBeGreaterThanOrEqual(5)
  })

  it('counts down and disables an unanswered player at local expiry', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-07-19T12:00:00Z'))
    saveSession('ABC234', player)
    vi.mocked(api.recoverSession).mockResolvedValue({ room: roomState('answering'), player })
    vi.mocked(api.getRoom).mockResolvedValue({ room: roomState('answering') })

    renderRoom()
    await act(async () => {})
    expect(screen.getByText('30s remaining')).toBeInTheDocument()
    expect(screen.getByLabelText('Your guess')).toBeEnabled()

    await act(async () => vi.advanceTimersByTime(30_000))
    expect(screen.getByText('Time expired', { selector: '[role="timer"]' })).toBeInTheDocument()
    expect(screen.getByLabelText('Your guess')).toBeDisabled()
    expect(screen.getByText('Answering has expired. The server will reveal the round automatically.')).toBeInTheDocument()
  })

  it('does not let a slower earlier poll overwrite a newer room response', async () => {
    vi.useFakeTimers()
    let resolveFirst!: (value: api.RoomResponse) => void
    vi.mocked(api.getRoom)
      .mockImplementationOnce(() => new Promise((resolve) => { resolveFirst = resolve }))
      .mockResolvedValueOnce({ room: roomState('revealed') })

    renderRoom()
    await act(async () => vi.advanceTimersByTime(5000))
    await act(async () => {})
    expect(screen.getByText('Answers and awarded scores are now revealed.')).toBeInTheDocument()

    await act(async () => resolveFirst({ room: roomState('lobby') }))
    expect(screen.getByText('Answers and awarded scores are now revealed.')).toBeInTheDocument()
    expect(screen.queryByText('Lobby')).not.toBeInTheDocument()
  })

  it('clears the recovered session and returns home', async () => {
    saveSession('ABC234', host)
    vi.mocked(api.recoverSession).mockResolvedValue({ room: roomState('lobby'), player: host })
    renderRoom()
    fireEvent.click(await screen.findByRole('button', { name: 'Leave this session' }))
    expect(screen.getByText('Home')).toBeInTheDocument()
    expect(window.localStorage.getItem('modelsays.session')).toBeNull()
  })

  it('copies completed results and lets only the host create a clean next lobby', async () => {
    saveSession('ABC234', host)
    const completed = roomState('completed', true)
    vi.mocked(api.recoverSession).mockResolvedValue({ room: completed, player: host })
    vi.mocked(api.getRoom).mockResolvedValue({ room: completed })
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
    Object.defineProperty(navigator, 'share', { configurable: true, value: undefined })
    vi.mocked(api.playAgain).mockResolvedValue({
      room: { ...roomState('lobby'), code: 'NEW234', name: 'Test room' },
      player: { ...host, id: 'new-host', token: 'new-token' },
    })

    renderRoom()
    fireEvent.click(await screen.findByRole('button', { name: 'Share results' }))
    expect(await screen.findByText('Replay link copied')).toBeInTheDocument()
    expect(writeText).toHaveBeenCalledWith(expect.stringContaining('/replay/replay-12345678901234567890123456789012'))

    fireEvent.click(screen.getByRole('button', { name: 'Play again' }))
    await waitFor(() => expect(api.playAgain).toHaveBeenCalledWith('ABC234', { playerToken: host.token }))
  })
})
