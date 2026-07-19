import { FormEvent, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'

import {
  getRoom,
  getJudgeSuggestions,
  nextRound,
  overrideMatch,
  recoverSession,
  revealRound,
  startGame,
  submitGuess,
  type Player,
  type JudgeSuggestion,
  type Room,
} from '../lib/api'
import { RoomEventClient, type RoomConnectionState } from '../lib/roomEvents'
import { clearSession, loadSession, saveSession } from '../lib/session'

const liveSafetyPollMilliseconds = 30_000
const fallbackPollMilliseconds = 5_000
const hiddenPollMilliseconds = 30_000

export function RoomPage() {
  const { code: routeCode = '' } = useParams()
  const code = routeCode.trim().toUpperCase()
  const navigate = useNavigate()
  const storedSession = useMemo(() => loadSession(), [])
  const matchingSession = storedSession?.roomCode.trim().toUpperCase() === code ? storedSession : null
  const [activePlayer, setActivePlayer] = useState<Player | null>(null)
  const [room, setRoom] = useState<Room | null>(null)
  const [isLoading, setIsLoading] = useState(true)
  const [isMutating, setIsMutating] = useState(false)
  const [errorMessage, setErrorMessage] = useState<string | null>(null)
  const [guessAnswer, setGuessAnswer] = useState('')
  const [overrideSelections, setOverrideSelections] = useState<Record<string, string>>({})
  const [judgeSuggestions, setJudgeSuggestions] = useState<JudgeSuggestion[]>([])
  const [connectionState, setConnectionState] = useState<RoomConnectionState>('connecting')
  const [now, setNow] = useState(() => Date.now())
  const requestSequence = useRef(0)
  const activeRequest = useRef<AbortController | null>(null)
  const mutationInFlight = useRef(false)
  const eventClient = useRef<RoomEventClient | null>(null)

  const refreshRoom = useCallback(async (showLoading = false) => {
    const sequence = ++requestSequence.current
    activeRequest.current?.abort()
    const controller = new AbortController()
    activeRequest.current = controller
    if (showLoading) setIsLoading(true)

    try {
      const response = await getRoom(code, controller.signal)
      if (sequence === requestSequence.current) {
        setRoom(response.room)
        eventClient.current?.setAppliedRevision(response.room.revision)
        setErrorMessage(null)
        return response.room.revision
      }
    } catch (error) {
      if (!controller.signal.aborted && sequence === requestSequence.current) {
        setErrorMessage(error instanceof Error ? error.message : 'Unable to load room')
      }
    } finally {
      if (sequence === requestSequence.current) {
        activeRequest.current = null
        if (showLoading) setIsLoading(false)
      }
    }
    return null
  }, [code])

  useEffect(() => {
    let cancelled = false
    const controller = new AbortController()
    const sequence = ++requestSequence.current
    activeRequest.current?.abort()
    activeRequest.current = controller

    async function load() {
      if (matchingSession?.player.token) {
        try {
          const response = await recoverSession(
            code,
            { playerToken: matchingSession.player.token },
            controller.signal,
          )
          if (cancelled || sequence !== requestSequence.current || !response.player) return
          setActivePlayer(response.player)
          saveSession(code, response.player)
          setRoom(response.room)
          setErrorMessage(null)
          setIsLoading(false)
          activeRequest.current = null
          return
        } catch (error) {
          if (controller.signal.aborted || sequence !== requestSequence.current) return
          clearSession()
          if (!cancelled) {
            setErrorMessage(error instanceof Error ? `Session expired: ${error.message}` : 'Session expired')
          }
        }
      }
      if (!cancelled && sequence === requestSequence.current) await refreshRoom(true)
    }

    void load()
    return () => {
      cancelled = true
      controller.abort()
      activeRequest.current?.abort()
    }
  }, [code, matchingSession?.player.token, refreshRoom])

  const currentGame = room?.currentGame
  const currentRound = currentGame?.currentRound
  const activeScore = currentGame?.scoreboard.find((entry) => entry.playerId === activePlayer?.id)
  const hasSubmitted = activeScore?.submissionMade ?? false
  const isGameCompleted = currentGame?.status === 'completed'
  const answerPhaseEndsAt = currentRound?.status === 'answering'
    ? Date.parse(currentRound.answerPhaseEndsAt)
    : Number.NaN
  const secondsRemaining = Number.isFinite(answerPhaseEndsAt)
    ? Math.max(0, Math.ceil((answerPhaseEndsAt - now) / 1000))
    : 0
  const hasLocallyExpired = currentRound?.status === 'answering' && secondsRemaining === 0
  const isHost = activePlayer?.isHost ?? false
  const activePlayerToken = activePlayer?.token ?? ''

  useEffect(() => {
    if (!activePlayerToken || !room || isGameCompleted) {
      if (isGameCompleted) setConnectionState('stopped')
      return
    }
    const client = new RoomEventClient({
      roomCode: code,
      playerToken: activePlayerToken,
      initialRevision: room.revision,
      onStateChange: setConnectionState,
      onConnected: async () => (await refreshRoom(false)) ?? room.revision,
      onInvalidations: async () => {
        if (mutationInFlight.current || document.visibilityState === 'hidden' || !navigator.onLine) {
          return room.revision
        }
        return (await refreshRoom(false)) ?? room.revision
      },
    })
    eventClient.current = client
    client.start()
    return () => {
      client.stop()
      if (eventClient.current === client) eventClient.current = null
    }
    // The stream cursor is updated through setAppliedRevision; room changes must not recreate it.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activePlayerToken, code, isGameCompleted, room != null, refreshRoom])

  useEffect(() => {
    if (!Number.isFinite(answerPhaseEndsAt) || hasLocallyExpired) return
    setNow(Date.now())
    const timer = window.setInterval(() => setNow(Date.now()), 250)
    return () => window.clearInterval(timer)
  }, [answerPhaseEndsAt, hasLocallyExpired])

  useEffect(() => {
    setGuessAnswer('')
    setOverrideSelections({})
    setNow(Date.now())
  }, [currentRound?.id])

  useEffect(() => {
    if (!isHost || !activePlayerToken || currentRound?.status !== 'revealed') {
      setJudgeSuggestions([])
      return
    }
    let cancelled = false
    void getJudgeSuggestions(code, currentRound.id, activePlayerToken)
      .then((response) => {
        if (!cancelled) setJudgeSuggestions(response.suggestions)
      })
      .catch((error) => {
        if (!cancelled) setErrorMessage(error instanceof Error ? error.message : 'Unable to load judge suggestions')
      })
    return () => { cancelled = true }
  }, [activePlayerToken, code, currentRound?.id, currentRound?.status, isHost, room?.updatedAt])

  useEffect(() => {
    if (isGameCompleted) return
    let timer: number | undefined
    let cancelled = false

    const schedule = () => {
      if (cancelled) return
      const delay = document.visibilityState === 'hidden'
        ? hiddenPollMilliseconds
        : connectionState === 'live'
          ? liveSafetyPollMilliseconds
          : fallbackPollMilliseconds
      timer = window.setTimeout(async () => {
        if (navigator.onLine && !mutationInFlight.current) await refreshRoom(false)
        schedule()
      }, delay)
    }
    const handleVisibility = () => {
      if (timer) window.clearTimeout(timer)
      if (document.visibilityState === 'visible' && navigator.onLine && !mutationInFlight.current) void refreshRoom(false)
      schedule()
    }
    const handleOnline = () => {
      if (!mutationInFlight.current) void refreshRoom(false)
    }

    schedule()
    document.addEventListener('visibilitychange', handleVisibility)
    window.addEventListener('online', handleOnline)
    return () => {
      cancelled = true
      if (timer) window.clearTimeout(timer)
      document.removeEventListener('visibilitychange', handleVisibility)
      window.removeEventListener('online', handleOnline)
    }
  }, [connectionState, isGameCompleted, refreshRoom])

  async function mutateRoom(action: () => Promise<unknown>) {
    mutationInFlight.current = true
    setIsMutating(true)
    setErrorMessage(null)
    requestSequence.current += 1
    activeRequest.current?.abort()

    try {
      await action()
      await refreshRoom(false)
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Action failed'
      await refreshRoom(false)
      setErrorMessage(message)
    } finally {
      mutationInFlight.current = false
      setIsMutating(false)
    }
  }

  async function handleStartGame() {
    if (!activePlayerToken) return setErrorMessage('Missing player session token')
    await mutateRoom(() => startGame(code, { playerToken: activePlayerToken }))
  }

  async function handleSubmitGuess(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!activePlayerToken || !currentRound) return setErrorMessage('Missing player session or round state')
    if (hasLocallyExpired) return setErrorMessage('Answer time has expired; waiting for the host to reveal')
    await mutateRoom(() => submitGuess(code, currentRound.id, {
      playerToken: activePlayerToken,
      answer: guessAnswer,
    }))
  }

  async function handleRevealRound() {
    if (!activePlayerToken || !currentRound) return setErrorMessage('Missing player session or round state')
    await mutateRoom(() => revealRound(code, currentRound.id, { playerToken: activePlayerToken }))
  }

  async function handleNextRound() {
    if (!activePlayerToken) return setErrorMessage('Missing player session token')
    await mutateRoom(() => nextRound(code, { playerToken: activePlayerToken }))
  }

  async function handleOverrideMatch(guessId: string) {
    if (!activePlayerToken || !currentRound) return setErrorMessage('Missing player session or round state')
    const selectedValue = overrideSelections[guessId] ?? ''
    const suggestion = judgeSuggestions.find((item) => item.guessId === guessId)
    await mutateRoom(() => overrideMatch(code, {
      playerToken: activePlayerToken,
      roundId: currentRound.id,
      guessId,
      matchedPredictionAnswerId: selectedValue || null,
      judgeSuggestionId: suggestion?.id,
    }))
  }

  const rankedScoreboard = [...(currentGame?.scoreboard ?? [])].sort((left, right) =>
    right.score - left.score || left.displayName.localeCompare(right.displayName),
  )
  const winningScore = rankedScoreboard[0]?.score
  const winnerCount = rankedScoreboard.filter((entry) => entry.score === winningScore).length
  const showLobbyState = room != null && !currentGame
  const showAnswerState = currentRound?.status === 'answering'
  const showRevealState = currentRound?.status === 'revealed'

  return (
    <section className="room-grid">
      <article className="panel room-overview">
        <div className="section-heading">
          <p className="eyebrow">Room state</p>
          <h1>{room?.name || code}</h1>
          <p>Live events trigger authoritative room refreshes, with polling recovery when the stream is unavailable.</p>
          <p aria-live="polite" className={`connection-indicator connection-${connectionState}`} role="status">
            {connectionState === 'live'
              ? 'Live updates connected'
              : connectionState === 'offline'
                ? 'Offline — updates will resume when your network returns'
                : connectionState === 'stopped'
                  ? 'Live updates complete'
                  : connectionState === 'fallback'
                    ? 'Reconnecting — polling for updates'
                    : 'Connecting live updates…'}
          </p>
        </div>

        <div className="room-code-row"><span>Room code</span><strong>{code}</strong></div>
        <div className="settings-grid">
          <div><span>Status</span><strong>{room?.status || 'loading'}</strong></div>
          <div><span>Mode</span><strong>{room?.settings.mode || 'simultaneous'}</strong></div>
          <div><span>Rounds</span><strong>{room?.settings.totalRounds ?? '—'}</strong></div>
          <div><span>Timer</span><strong>{room ? `${room.settings.answerTimerSeconds}s` : '—'}</strong></div>
          <div><span>Locale</span><strong>{room?.settings.locale || '—'}</strong></div>
          <div><span>Model</span><strong>{room?.settings.predictionModel || '—'}</strong></div>
        </div>

        <div className="action-row">
          <button className="button button-secondary" onClick={() => void refreshRoom(false)} type="button">Refresh room state</button>
          {activePlayer ? (
            <button className="button button-secondary" onClick={() => { clearSession(); navigate('/') }} type="button">
              Leave this session
            </button>
          ) : null}
        </div>

        {showLobbyState ? (
          <section className="phase-card">
            <h2>Lobby</h2>
            <p className="info-note">Start when everyone has joined. New players cannot join after the game begins.</p>
            {isHost ? (
              <button className="button button-primary" disabled={isMutating} onClick={() => void handleStartGame()} type="button">
                {isMutating ? 'Starting…' : 'Start game'}
              </button>
            ) : <p className="status-note">Waiting for the host to start the game.</p>}
          </section>
        ) : null}

        {showAnswerState && currentRound ? (
          <section className="phase-card">
            <div className="round-meta">
              <span className="eyebrow">Round {currentRound.roundIndex}</span>
              <strong>{currentRound.roundIndex}/{currentGame?.totalRounds}</strong>
            </div>
            <h2>{currentRound.question.text}</h2>
            <p className="countdown" role="timer">{hasLocallyExpired ? 'Time expired' : `${secondsRemaining}s remaining`}</p>
            <p className="status-note">
              {hasSubmitted
                ? 'Submitted. Your guess is locked while you wait for the host.'
                : hasLocallyExpired
                  ? 'Answering has expired. Waiting for the host to reveal.'
                  : 'Accepting answers. The board stays hidden until the host reveals it.'}
            </p>
            <form className="answer-form" onSubmit={handleSubmitGuess}>
              <input
                aria-label="Your guess"
                disabled={hasSubmitted || hasLocallyExpired || isMutating || !activePlayerToken}
                maxLength={120}
                onChange={(event) => setGuessAnswer(event.target.value)}
                placeholder="Type your guess"
                value={guessAnswer}
              />
              <button className="button button-primary" disabled={hasSubmitted || hasLocallyExpired || isMutating || !guessAnswer.trim()} type="submit">
                {hasSubmitted ? 'Submitted' : hasLocallyExpired ? 'Time expired' : isMutating ? 'Submitting…' : 'Submit guess'}
              </button>
            </form>
            {isHost ? (
              <div className="action-row">
                <button className="button button-secondary" disabled={isMutating} onClick={() => void handleRevealRound()} type="button">Reveal round</button>
              </div>
            ) : null}
          </section>
        ) : null}

        {showRevealState && currentRound ? (
          <section className="phase-card">
            <div className="round-meta"><span className="eyebrow">Revealed</span><strong>Board hash {currentRound.boardHash}</strong></div>
            <h2>{currentRound.question.text}</h2>
            <p className="status-note">Answers and awarded scores are now revealed.</p>
            <div className="answer-board">
              {currentRound.board?.answers.map((answer) => (
                <div className="answer-row" key={answer.id}><span>#{answer.rank}</span><strong>{answer.canonicalAnswer}</strong><em>{answer.score} pts</em></div>
              ))}
            </div>
            <div className="guess-list">
              <h3>Guesses</h3>
              {currentRound.guesses?.length ? currentRound.guesses.map((guess) => (
                <div className="guess-row" key={guess.id}>
                  <div className="guess-main">
                    <strong>{guess.playerDisplayName}</strong><span>{guess.rawAnswer}</span>
                    <span>{guess.duplicate ? 'Duplicate answer' : guess.matchedPredictionAnswerId ? 'Matched' : 'Miss'}</span>
                  </div>
                  <div className="guess-actions">
                    <em>{guess.scoreAwarded} pts</em>
                    {isHost && currentRound.board ? (
                      <>
                        {judgeSuggestions.find((item) => item.guessId === guess.id && item.outcome === 'suggestion') ? (() => {
                          const suggestion = judgeSuggestions.find((item) => item.guessId === guess.id)!
                          const answer = currentRound.board?.answers.find((item) => item.id === suggestion.suggestedPredictionAnswerId)
                          return (
                            <span className="status-note">
                              Judge suggests {answer?.canonicalAnswer ?? 'an answer'} ({suggestion.confidenceBand}, {Math.round(suggestion.confidence * 100)}%; {suggestion.rationaleCategory})
                            </span>
                          )
                        })() : null}
                        <select aria-label={`Override ${guess.playerDisplayName}`} className="override-select" onChange={(event) => setOverrideSelections((current) => ({ ...current, [guess.id]: event.target.value }))} value={overrideSelections[guess.id] ?? guess.matchedPredictionAnswerId ?? ''}>
                          <option value="">Mark as miss</option>
                          {currentRound.board.answers.map((answer) => <option key={answer.id} value={answer.id}>{answer.rank}. {answer.canonicalAnswer}</option>)}
                        </select>
                        <button className="button button-secondary" disabled={isMutating} onClick={() => void handleOverrideMatch(guess.id)} type="button">Apply override</button>
                      </>
                    ) : null}
                  </div>
                </div>
              )) : <p className="status-note">No guesses were submitted this round.</p>}
            </div>
            <p className="info-note">{isGameCompleted ? 'Game complete. Final scoreboard below.' : 'Waiting for the host to begin the next round.'}</p>
            {isHost && !isGameCompleted ? (
              <div className="action-row">
                <button className="button button-primary" disabled={isMutating} onClick={() => void handleNextRound()} type="button">
                  {currentGame && currentGame.currentRoundIndex >= currentGame.totalRounds ? 'Finish game' : 'Next round'}
                </button>
              </div>
            ) : null}
          </section>
        ) : null}

        {isLoading ? <p className="status-note">Loading room…</p> : null}
        {isMutating ? <p className="status-note">Updating room…</p> : null}
        {errorMessage ? <p className="form-error">{errorMessage}</p> : null}
      </article>

      <aside className="panel player-panel">
        <div className="section-heading compact-heading"><p className="eyebrow">Players</p><h2>{room?.players.length ?? 0} in room</h2></div>
        <ul className="player-list">
          {room?.players.map((player) => (
            <li key={player.id} className={player.id === activePlayer?.id ? 'player-card player-card-active' : 'player-card'}>
              <div><strong>{player.displayName}</strong><span>{player.isHost ? 'Host' : 'Player'}</span></div>
              {player.id === activePlayer?.id ? <em>You</em> : null}
            </li>
          ))}
        </ul>
        {currentGame ? (
          <div className="score-list">
            <div className="section-heading compact-heading"><p className="eyebrow">Scores</p><h2>{isGameCompleted ? 'Final scoreboard' : 'Live scoreboard'}</h2></div>
            {rankedScoreboard.map((entry, index) => {
              const isWinner = isGameCompleted && entry.score === winningScore
              return (
                <div className={isWinner ? 'score-row score-row-winner' : 'score-row'} key={entry.playerId}>
                  <strong className="score-rank">#{index + 1}</strong>
                  <div>
                    <strong>{entry.displayName}{isWinner ? winnerCount > 1 ? ' — tied winner' : ' — winner' : ''}</strong>
                    <span>{currentRound?.status === 'answering' ? entry.submissionMade ? 'Submitted' : 'Waiting' : 'Revealed'}</span>
                  </div>
                  <em>{entry.score} pts</em>
                </div>
              )
            })}
          </div>
        ) : null}
      </aside>
    </section>
  )
}
