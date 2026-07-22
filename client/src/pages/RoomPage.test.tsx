import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import axe from 'axe-core'
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
    passTurn: vi.fn(),
    nextRound: vi.fn(),
    playAgain: vi.fn(),
    createTeam: vi.fn(),
    assignTeam: vi.fn(),
    overrideMatch: vi.fn(),
    getJudgeSuggestions: vi.fn(),
  }
})

vi.mock('qrcode', () => ({
  default: {
    toDataURL: vi.fn(async (value: string) => `data:image/png;base64,${btoa(value)}`),
  },
}))

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
  predictionModel: 'gpt-5.6-luna',
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
  it('renders a non-playing living-room TV lobby with a safe QR join URL', async () => {
    const focus = vi.spyOn(HTMLElement.prototype, 'focus')
    const display = { ...host, role: 'host_display' as const }
    const livingRoom = roomState('lobby')
    livingRoom.settings = { ...settings, mode: 'livingroom' }
    livingRoom.players = [display, { ...player, role: 'participant' }, { ...player, id: 'player-2', displayName: 'Second', role: 'participant' }]
    saveSession('ABC234', display)
    vi.mocked(api.recoverSession).mockResolvedValue({ room: livingRoom, player: display })
    vi.mocked(api.getRoom).mockResolvedValue({ room: livingRoom })
    vi.mocked(api.startGame).mockResolvedValue({ room: livingRoom })

    renderRoom()

    expect(await screen.findByRole('heading', { name: '2 players joined' })).toBeInTheDocument()
    const qr = await screen.findByRole('img', { name: /QR code for/i })
    expect(qr.getAttribute('alt')).toBe('QR code for http://localhost:3000/join?code=ABC234')
    expect(qr.getAttribute('src')).toMatch(/^data:image\/png;base64,/)
    expect(qr.getAttribute('alt')).not.toMatch(/token|replay|provider/i)
    fireEvent.click(screen.getByRole('button', { name: 'Start game' }))
    await waitFor(() => expect(api.startGame).toHaveBeenCalledWith('ABC234', { playerToken: 'host-token' }))
    expect(screen.queryByLabelText('Your guess')).not.toBeInTheDocument()
    expect(focus).toHaveBeenCalledWith({ preventScroll: true })
  })

  it('lets the living-room TV return home from the final scoreboard', async () => {
    const display = { ...host, role: 'host_display' as const }
    const completed = roomState('completed')
    completed.settings = { ...settings, mode: 'livingroom' }
    completed.players = [display, { ...player, role: 'participant' }]
    completed.currentGame!.mode = 'livingroom'
    completed.currentGame!.scoreboard = [{
      playerId: player.id,
      displayName: player.displayName,
      isHost: false,
      score: 50,
      submissionMade: true,
    }]
    saveSession('ABC234', display)
    vi.mocked(api.recoverSession).mockResolvedValue({ room: completed, player: display })

    renderRoom()

    expect(await screen.findByRole('heading', { name: 'Final scoreboard' })).toBeInTheDocument()
    expect(screen.queryByText('Host')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Back to home' }))
    expect(await screen.findByText('Home')).toBeInTheDocument()
  })

  it('has no serious or critical accessibility violations in a maximum-player lobby', async () => {
    saveSession('ABC234', host)
    const maximumPlayerRoom = roomState('lobby')
    maximumPlayerRoom.name = 'A deliberately long room name that reaches the maximum'
    maximumPlayerRoom.players = Array.from({ length: 12 }, (_, index) => ({
      ...player,
      id: `player-${index}`,
      displayName: `Long player name ${String(index + 1).padStart(2, '0')}`,
      isHost: index === 0,
      token: index === 0 ? host.token : `token-${index}`,
    }))
    const recoveredHost = maximumPlayerRoom.players[0]
    vi.mocked(api.recoverSession).mockResolvedValue({ room: maximumPlayerRoom, player: recoveredHost })

    const { container } = renderRoom()
    await screen.findByRole('heading', { name: 'Lobby' })
    expect(screen.getAllByRole('listitem')).toHaveLength(12)
    const result = await axe.run(container, { rules: { 'color-contrast': { enabled: false } } })
    expect(result.violations.filter((violation) => ['serious', 'critical'].includes(violation.impact ?? ''))).toEqual([])
  })

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
      model: 'gpt-5.6-luna',
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
    expect(screen.getByText('Answering has expired. Waiting for the round result.')).toBeInTheDocument()
  })

  it.each([
    ['lobby', 'Waiting for the host'],
    ['answering', 'Name a fruit'],
    ['revealed', 'Result is on the host display'],
  ] as const)('keeps the participant %s surface focused on the current action', async (phase, heading) => {
    saveSession('ABC234', player)
    const current = roomState(phase)
    vi.mocked(api.recoverSession).mockResolvedValue({ room: current, player })
    vi.mocked(api.getRoom).mockResolvedValue({ room: current })

    const { container } = renderRoom()
    const phaseHeading = await screen.findByRole('heading', { name: heading })
    await waitFor(() => expect(phaseHeading).toHaveFocus())
    expect(container.querySelector('.participant-room')).toBeInTheDocument()
    expect(container.querySelector('.room-grid')).not.toBeInTheDocument()
    expect(container.querySelector('aside')).not.toBeInTheDocument()
    expect(screen.queryByText('Room state')).not.toBeInTheDocument()
    expect(screen.queryByText('Players')).not.toBeInTheDocument()
    expect(screen.queryByText('Live scoreboard')).not.toBeInTheDocument()
    expect(screen.queryByText('Settings')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Copy invite' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Presentation mode' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Refresh room state' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Reveal round' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Next round' })).not.toBeInTheDocument()
    expect(screen.queryByText('Guesses')).not.toBeInTheDocument()
    expect(screen.queryByText('Apple')).not.toBeInTheDocument()
  })

  it('shows a submitted participant only the locked answering action', async () => {
    saveSession('ABC234', player)
    const current = roomState('answering')
    current.currentGame!.scoreboard[1].submissionMade = true
    vi.mocked(api.recoverSession).mockResolvedValue({ room: current, player })
    vi.mocked(api.getRoom).mockResolvedValue({ room: current })

    renderRoom()
    expect(await screen.findByText('Submitted. Your guess is locked while you wait for the host.')).toBeInTheDocument()
    expect(screen.getByLabelText('Your guess')).toBeDisabled()
    expect(screen.queryByText('Live scoreboard')).not.toBeInTheDocument()
  })

  it('shows participants only individual and team final rankings with safe actions', async () => {
    saveSession('ABC234', player)
    const completed = roomState('completed', true)
    completed.settings = { ...settings, mode: 'teams' }
    completed.currentGame!.mode = 'teams'
    completed.currentGame!.teamScoreboard = [
      { teamId: 'blue', name: 'A very long blue team name', score: 50 },
      { teamId: 'gold', name: 'Gold', score: 50 },
    ]
    vi.mocked(api.recoverSession).mockResolvedValue({ room: completed, player })
    vi.mocked(api.getRoom).mockResolvedValue({ room: completed })

    renderRoom()
    expect(await screen.findByRole('heading', { name: 'Final scoreboard' })).toBeInTheDocument()
    expect(screen.getByText('Player — tied winner')).toBeInTheDocument()
    expect(screen.getByText('Final team ranking')).toBeInTheDocument()
    expect(screen.getByText('A very long blue team name — tied team winner')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Share results' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'View replay' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Back to home' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Play again' })).not.toBeInTheDocument()
    expect(screen.queryByText('Answers and awarded scores are now revealed.')).not.toBeInTheDocument()
    expect(screen.queryByText('Players')).not.toBeInTheDocument()
    expect(screen.queryByText('Room code')).not.toBeInTheDocument()
  })

  it.each(['lobby', 'answering', 'revealed', 'completed'] as const)(
    'has no serious or critical accessibility violations in the participant %s state',
    async (phase) => {
      saveSession('ABC234', player)
      const current = roomState(phase, phase === 'completed')
      vi.mocked(api.recoverSession).mockResolvedValue({ room: current, player })
      vi.mocked(api.getRoom).mockResolvedValue({ room: current })

      const { container } = renderRoom()
      await waitFor(() => expect(screen.queryByText('Loading your game…')).not.toBeInTheDocument())
      const result = await axe.run(container, { rules: { 'color-contrast': { enabled: false } } })
      expect(result.violations.filter((violation) => ['serious', 'critical'].includes(violation.impact ?? ''))).toEqual([])
    },
  )

  it('keeps participant errors assertive without exposing host controls', async () => {
    saveSession('ABC234', player)
    const current = roomState('answering')
    vi.mocked(api.recoverSession).mockResolvedValue({ room: current, player })
    vi.mocked(api.getRoom).mockResolvedValue({ room: current })
    vi.mocked(api.submitGuess).mockRejectedValue(new Error('Connection interrupted'))

    renderRoom()
    fireEvent.change(await screen.findByLabelText('Your guess'), { target: { value: 'Apple' } })
    fireEvent.click(screen.getByRole('button', { name: 'Submit guess' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Connection interrupted')
    expect(screen.queryByRole('button', { name: 'Refresh room state' })).not.toBeInTheDocument()
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

  it('focuses the current phase, copies an invite, and announces presentation state', async () => {
    saveSession('ABC234', host)
    vi.mocked(api.recoverSession).mockResolvedValue({ room: roomState('lobby'), player: host })
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
    const requestFullscreen = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(document.documentElement, 'requestFullscreen', { configurable: true, value: requestFullscreen })

    renderRoom()
    const heading = await screen.findByRole('heading', { name: 'Lobby' })
    await waitFor(() => expect(heading).toHaveFocus())
    fireEvent.click(screen.getByRole('button', { name: 'Copy invite' }))
    await waitFor(() => expect(writeText).toHaveBeenCalledWith('http://localhost:3000/join?code=ABC234'))
    expect(screen.getByText(/Invite link copied/)).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Presentation mode' }))
    await waitFor(() => expect(requestFullscreen).toHaveBeenCalled())
    expect(screen.getByText(/Presentation mode enabled/)).toBeInTheDocument()
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

  it('lets the host create and assign teams in the lobby', async () => {
    saveSession('ABC234', host)
    const teamRoom: api.Room = {
      ...roomState('lobby'),
      settings: { ...settings, mode: 'teams' },
      teams: [{ id: 'blue', name: 'Blue' }, { id: 'gold', name: 'Gold' }],
    }
    vi.mocked(api.recoverSession).mockResolvedValue({ room: teamRoom, player: host })
    vi.mocked(api.getRoom).mockResolvedValue({ room: teamRoom })
    vi.mocked(api.createTeam).mockResolvedValue({ room: teamRoom })
    vi.mocked(api.assignTeam).mockResolvedValue({ room: teamRoom })
    renderRoom()

    fireEvent.change(await screen.findByLabelText('New team name'), { target: { value: 'Green' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create team' }))
    await waitFor(() => expect(api.createTeam).toHaveBeenCalledWith('ABC234', { playerToken: host.token, name: 'Green' }))

    fireEvent.change(screen.getByLabelText('Team for Player'), { target: { value: 'gold' } })
    await waitFor(() => expect(api.assignTeam).toHaveBeenCalledWith('ABC234', player.id, { playerToken: host.token, teamId: 'gold' }))
  })

  it('shows waiting and active sequential turn states with prior claims and pass', async () => {
    saveSession('ABC234', player)
    const sequential = roomState('answering')
    sequential.settings = { ...settings, mode: 'sequential' }
    sequential.currentGame!.mode = 'sequential'
    sequential.currentGame!.currentRound = {
      ...sequential.currentGame!.currentRound!,
      turnOrder: [host.id, player.id],
      currentTurnIndex: 0,
      turnEndsAt: new Date(Date.now() + 30_000).toISOString(),
      guesses: [{ id: 'prior', playerId: host.id, playerDisplayName: 'Host', rawAnswer: 'Apple', normalizedAnswer: '', scoreAwarded: 0, duplicate: false, createdAt: '2026-07-19T12:00:01Z' }],
    }
    vi.mocked(api.recoverSession).mockResolvedValue({ room: sequential, player })
    vi.mocked(api.getRoom).mockResolvedValue({ room: sequential })
    vi.mocked(api.passTurn).mockResolvedValue({ room: sequential })
    const firstView = renderRoom()
    expect(await screen.findByText('Waiting for Host to answer.')).toBeInTheDocument()
    expect(screen.getByLabelText('Your guess')).toBeDisabled()
    expect(screen.getByText('Prior claims')).toBeInTheDocument()

    firstView.unmount()
    sequential.currentGame!.currentRound!.currentTurnIndex = 1
    vi.mocked(api.recoverSession).mockResolvedValue({ room: sequential, player })
    renderRoom()
    fireEvent.click(await screen.findByRole('button', { name: 'Pass turn' }))
    await waitFor(() => expect(api.passTurn).toHaveBeenCalledWith('ABC234', 'round-1', { playerToken: player.token }))
  })
})
