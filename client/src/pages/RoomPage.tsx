import { FormEvent, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'

import {
  assignTeam,
  createTeam,
  getRoom,
  getJudgeSuggestions,
  nextRound,
  playAgain,
  overrideMatch,
  passTurn,
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
  const [selectedOptionId, setSelectedOptionId] = useState('')
  const [overrideSelections, setOverrideSelections] = useState<Record<string, string>>({})
  const [judgeSuggestions, setJudgeSuggestions] = useState<JudgeSuggestion[]>([])
  const [connectionState, setConnectionState] = useState<RoomConnectionState>('connecting')
  const [shareStatus, setShareStatus] = useState('')
  const [presentationStatus, setPresentationStatus] = useState('')
  const [teamName, setTeamName] = useState('')
  const [joinQRCode, setJoinQRCode] = useState('')
  const [now, setNow] = useState(() => Date.now())
  const requestSequence = useRef(0)
  const activeRequest = useRef<AbortController | null>(null)
  const mutationInFlight = useRef(false)
  const eventClient = useRef<RoomEventClient | null>(null)
  const phaseHeading = useRef<HTMLHeadingElement | null>(null)

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
  const isOpenTrivia = currentGame?.gameKind === 'trivia_open'
  const isChoiceTrivia = currentGame?.gameKind === 'trivia_choice'
  const isTrivia = isOpenTrivia || isChoiceTrivia
  const activeScore = currentGame?.scoreboard.find((entry) => entry.playerId === activePlayer?.id)
  const hasSubmitted = activeScore?.submissionMade ?? false
  const isGameCompleted = currentGame?.status === 'completed'
  const isSequential = currentGame?.mode === 'sequential'
  const currentTurnPlayerId = currentRound?.currentTurnIndex == null
    ? undefined
    : currentRound.turnOrder?.[currentRound.currentTurnIndex]
  const currentTurnPlayer = room?.players.find((player) => player.id === currentTurnPlayerId)
  const isActiveTurn = !isSequential || currentTurnPlayerId === activePlayer?.id
  const answerPhaseEndsAt = currentRound?.status === 'answering'
    ? Date.parse(isSequential && currentRound.turnEndsAt ? currentRound.turnEndsAt : currentRound.answerPhaseEndsAt)
    : Number.NaN
  const secondsRemaining = Number.isFinite(answerPhaseEndsAt)
    ? Math.max(0, Math.ceil((answerPhaseEndsAt - now) / 1000))
    : 0
  const hasLocallyExpired = currentRound?.status === 'answering' && secondsRemaining === 0
  const isHost = activePlayer?.isHost ?? false
  const isLivingRoomDisplay = activePlayer?.role === 'host_display' && room?.settings.mode === 'livingroom'
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
    if (!isLivingRoomDisplay || currentRound?.status !== 'revealed' || !currentRound.revealPhaseEndsAt) return
    const timer = window.setInterval(() => setNow(Date.now()), 250)
    return () => window.clearInterval(timer)
  }, [currentRound?.revealPhaseEndsAt, currentRound?.status, isLivingRoomDisplay])

  useEffect(() => {
    setGuessAnswer('')
    setSelectedOptionId('')
    setOverrideSelections({})
    setNow(Date.now())
  }, [currentRound?.id])

  useEffect(() => {
    if (!isLoading && (currentRound?.status || room?.status)) phaseHeading.current?.focus({ preventScroll: true })
  }, [currentRound?.id, currentRound?.status, isLoading, room?.status])

  useEffect(() => {
    if (!isHost || isTrivia || !activePlayerToken || currentRound?.status !== 'revealed') {
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
  }, [activePlayerToken, code, currentRound?.id, currentRound?.status, isHost, isTrivia, room?.updatedAt])

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

  async function handleCreateTeam(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!activePlayerToken) return setErrorMessage('Missing player session token')
    await mutateRoom(() => createTeam(code, { playerToken: activePlayerToken, name: teamName }))
    setTeamName('')
  }

  async function handleAssignTeam(playerId: string, teamId: string) {
    if (!activePlayerToken) return setErrorMessage('Missing player session token')
    await mutateRoom(() => assignTeam(code, playerId, { playerToken: activePlayerToken, teamId }))
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

  async function handleChoiceSubmit(optionId: string) {
    if (!activePlayerToken || !currentRound) return setErrorMessage('Missing player session or round state')
    if (hasSubmitted || mutationInFlight.current) return
    if (hasLocallyExpired) return setErrorMessage('Answer time has expired; waiting for the round result')
    setSelectedOptionId(optionId)
    await mutateRoom(() => submitGuess(code, currentRound.id, {
      playerToken: activePlayerToken,
      optionId,
    }))
  }

  async function handleRevealRound() {
    if (!activePlayerToken || !currentRound) return setErrorMessage('Missing player session or round state')
    await mutateRoom(() => revealRound(code, currentRound.id, { playerToken: activePlayerToken }))
  }

  async function handlePassTurn() {
    if (!activePlayerToken || !currentRound) return setErrorMessage('Missing player session or round state')
    await mutateRoom(() => passTurn(code, currentRound.id, { playerToken: activePlayerToken }))
  }

  async function handleNextRound() {
    if (!activePlayerToken) return setErrorMessage('Missing player session token')
    await mutateRoom(() => nextRound(code, { playerToken: activePlayerToken }))
  }

  async function handlePlayAgain() {
    if (!activePlayerToken) return setErrorMessage('Missing player session token')
    setIsMutating(true)
    try {
      const response = await playAgain(code, { playerToken: activePlayerToken })
      if (!response.player?.token) throw new Error('New host session was not returned')
      saveSession(response.room.code, response.player)
      navigate(`/room/${response.room.code}`)
    } catch (error) {
      setErrorMessage(error instanceof Error ? error.message : 'Unable to create a new room')
      setIsMutating(false)
    }
  }

  async function handleShareResults() {
    if (!currentGame?.replayId) return
    const url = `${window.location.origin}/replay/${currentGame.replayId}`
    try {
      if (navigator.share) {
        await navigator.share({ title: `${room?.name ?? 'Model Says'} results`, url })
        setShareStatus('Results shared')
      } else {
        await navigator.clipboard.writeText(url)
        setShareStatus('Replay link copied')
      }
    } catch (error) {
      if ((error as DOMException)?.name !== 'AbortError') setShareStatus('Unable to share results')
    }
  }

  async function handleCopyInvite() {
    try {
      await navigator.clipboard.writeText(`${window.location.origin}/join?code=${code}`)
      setShareStatus('Invite link copied')
    } catch {
      setShareStatus(`Copy unavailable. Share room code ${code}.`)
    }
  }

  async function handleFullscreen() {
    try {
      if (document.fullscreenElement) {
        await document.exitFullscreen()
        setPresentationStatus('Exited presentation mode')
      } else {
        await document.documentElement.requestFullscreen()
        setPresentationStatus('Presentation mode enabled')
      }
    } catch {
      setPresentationStatus('Fullscreen is unavailable in this browser')
    }
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

  async function handleTriviaOverride(guessId: string, correct: boolean) {
    if (!activePlayerToken || !currentRound) return setErrorMessage('Missing player session or round state')
    await mutateRoom(() => overrideMatch(code, {
      playerToken: activePlayerToken,
      roundId: currentRound.id,
      guessId,
      correct,
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
  const activeGuess = currentRound?.guesses?.find((guess) => guess.playerId === activePlayer?.id)
  const choiceOptions = currentRound?.triviaContent?.options ?? []
  const revealedSelectedOptionId = activeGuess?.selectedOptionId ?? selectedOptionId
  const selectedChoiceLabel = choiceOptions.find((option) => option.id === revealedSelectedOptionId)?.label
  const correctChoiceLabel = choiceOptions.find((option) => option.id === currentRound?.triviaContent?.correctOptionId)?.label
  const joinURL = `${window.location.origin}/join?code=${encodeURIComponent(code)}`
  useEffect(() => {
    if (!isLivingRoomDisplay || !showLobbyState) {
      setJoinQRCode('')
      return
    }
    let cancelled = false
    void import('qrcode')
      .then(({ default: QRCode }) => QRCode.toDataURL(joinURL, { errorCorrectionLevel: 'M', margin: 2, width: 320 }))
      .then((value) => { if (!cancelled) setJoinQRCode(value) })
      .catch(() => { if (!cancelled) setJoinQRCode('') })
    return () => { cancelled = true }
  }, [isLivingRoomDisplay, joinURL, showLobbyState])
  const connectionMessage = connectionState === 'live'
    ? 'Live updates connected'
    : connectionState === 'offline'
      ? 'Offline — updates will resume when your network returns'
      : connectionState === 'stopped'
        ? 'Live updates complete'
        : connectionState === 'fallback'
          ? 'Reconnecting — polling for updates'
          : 'Connecting live updates…'

  if (isLivingRoomDisplay) {
    const participants = room?.players.filter((player) => player.role !== 'host_display') ?? []
    const revealSeconds = currentRound?.revealPhaseEndsAt
      ? Math.max(0, Math.ceil((Date.parse(currentRound.revealPhaseEndsAt) - now) / 1000))
      : 0
    return (
      <section className="livingroom-display" aria-busy={isLoading || isMutating}>
        <article className="panel livingroom-surface">
          <div className="livingroom-topbar">
            <div><p className="eyebrow">Living-room game</p><h1>{room?.name}</h1></div>
            <p aria-live="polite" className={`connection-indicator connection-${connectionState}`} role="status">{connectionMessage}</p>
            <button className="button button-secondary" onClick={() => void handleFullscreen()} type="button">Fullscreen</button>
          </div>
          {showLobbyState ? (
            <section className="livingroom-lobby">
              <div>
                <p className="eyebrow">Scan to join</p>
                <div className="room-code-row"><span>Room code</span><strong>{code}</strong></div>
                {joinQRCode
                  ? <img alt={`QR code for ${joinURL}`} className="join-qr" src={joinQRCode} />
                  : <p className="status-note">QR unavailable. Join at {joinURL}</p>}
                <button className="button button-secondary" onClick={() => void handleCopyInvite()} type="button">Copy join link</button>
              </div>
              <div>
                <h2 ref={phaseHeading} tabIndex={-1}>{participants.length} players joined</h2>
                <ul className="player-list">{participants.map((player) => <li className="player-card" key={player.id}><strong>{player.displayName}</strong></li>)}</ul>
                <button className="button button-primary" disabled={isMutating || participants.length < 2} onClick={() => void handleStartGame()} type="button">
                  {participants.length < 2 ? 'Waiting for 2 players' : isMutating ? 'Starting…' : 'Start game'}
                </button>
              </div>
            </section>
          ) : null}
          {showAnswerState && currentRound ? (
            <section className="livingroom-question">
              <p className="eyebrow">Round {currentRound.roundIndex} of {currentGame?.totalRounds}</p>
              <h2 ref={phaseHeading} tabIndex={-1}>{currentRound.question.text}</h2>
              {isChoiceTrivia ? (
                <div aria-label="Answer choices" className="choice-grid livingroom-choice-grid">
                  {choiceOptions.map((option) => <div className="choice-button" key={option.id}>{option.label}</div>)}
                </div>
              ) : null}
              <p className="countdown" role="timer">{hasLocallyExpired ? 'Revealing…' : `${secondsRemaining}s`}</p>
              <p className="status-note">{currentGame?.scoreboard.filter((entry) => entry.submissionMade).length ?? 0} of {participants.length} answered</p>
            </section>
          ) : null}
          {showRevealState && currentRound && !isGameCompleted ? (
            <section className="phase-card reveal-card">
              <p className="eyebrow">Results · next round in {revealSeconds}s</p>
              <h2 ref={phaseHeading} tabIndex={-1}>{currentRound.question.text}</h2>
              {isTrivia ? (
                <div className="livingroom-trivia-result">
                  <p><span>Correct answer</span><strong>{isChoiceTrivia ? correctChoiceLabel : currentRound.triviaContent?.canonicalAnswer}</strong></p>
                  {currentRound.triviaContent?.explanation ? <p className="status-note">{currentRound.triviaContent.explanation}</p> : null}
                  <div className="guess-list">
                    <h3>Round results</h3>
                    {currentRound.guesses?.length ? currentRound.guesses.map((guess) => {
                      const answer = isChoiceTrivia
                        ? choiceOptions.find((option) => option.id === guess.selectedOptionId)?.label || 'No answer'
                        : 'Answer submitted'
                      return <div className="guess-row" key={guess.id}><span><strong>{guess.playerDisplayName}</strong>: {answer}</span><em>{guess.correct ? 'Correct' : 'Incorrect'} · +{guess.scoreAwarded} pts</em></div>
                    }) : <p>No answers were submitted.</p>}
                  </div>
                </div>
              ) : <div className="answer-board">{currentRound.board?.answers.map((answer) => <div className="answer-row" key={answer.id}><span>#{answer.rank}</span><strong>{answer.canonicalAnswer}</strong><em>{answer.score} pts</em></div>)}</div>}
              <div className="score-list">{rankedScoreboard.map((entry, index) => <div className="score-row" key={entry.playerId}><strong>#{index + 1}</strong><span>{entry.displayName}</span><em>{entry.score} pts</em></div>)}</div>
            </section>
          ) : null}
          {isGameCompleted && currentGame ? (
            <section className="livingroom-question">
              <p className="eyebrow">Game complete</p><h2 ref={phaseHeading} tabIndex={-1}>Final scoreboard</h2>
              <div className="score-list">{rankedScoreboard.map((entry, index) => <div className={entry.score === winningScore ? 'score-row score-row-winner' : 'score-row'} key={entry.playerId}><strong>#{index + 1}</strong><span>{entry.displayName}</span><em>{entry.score} pts</em></div>)}</div>
              <div className="action-row livingroom-final-actions">
                <button className="button button-secondary" disabled={isMutating} onClick={() => void handlePlayAgain()} type="button">{isMutating ? 'Creating…' : 'Play again'}</button>
                <button className="button button-primary" onClick={() => navigate('/')} type="button">Back to home</button>
              </div>
            </section>
          ) : null}
          <p aria-live="polite" className="status-note" role="status">{shareStatus} {presentationStatus}</p>
          {errorMessage ? <p className="form-error" role="alert">{errorMessage}</p> : null}
        </article>
      </section>
    )
  }

  if (matchingSession && !matchingSession.player.isHost && !activePlayer) {
    return (
      <section className="participant-room">
        <article aria-busy="true" className="panel participant-surface">
          <p aria-live="polite" className="status-note" role="status">Loading your game…</p>
          {errorMessage ? <p className="form-error" role="alert">{errorMessage}</p> : null}
        </article>
      </section>
    )
  }

  if (activePlayer && !isHost) {
    return (
      <section className="participant-room" aria-busy={isLoading || isMutating}>
        <article className="panel participant-surface">
          {!isGameCompleted ? (
            <p aria-live="polite" className={`connection-indicator connection-${connectionState}`} role="status">
              {connectionMessage}
            </p>
          ) : null}

          {showLobbyState ? (
            <section className="participant-phase">
              <p className="eyebrow">You joined</p>
              <h1>{room.name}</h1>
              <div className="room-code-row"><span>Room code</span><strong>{code}</strong></div>
              <h2 ref={phaseHeading} tabIndex={-1}>Waiting for the host</h2>
              <p className="status-note">You’re in as {activePlayer.displayName}. The game will appear here when the host starts it.</p>
            </section>
          ) : null}

          {showAnswerState && currentRound ? (
            <section className="participant-phase">
              <div className="round-meta">
                <span className="eyebrow">Round {currentRound.roundIndex}</span>
                <strong>{currentRound.roundIndex}/{currentGame?.totalRounds}</strong>
              </div>
              <h1 ref={phaseHeading} tabIndex={-1}>{currentRound.question.text}</h1>
              <p className="countdown" role="timer">{hasLocallyExpired ? 'Time expired' : `${secondsRemaining}s remaining`}</p>
              <p aria-atomic="true" aria-live="polite" className="status-note" role="status">
                {isSequential && !isActiveTurn
                  ? `Waiting for ${currentTurnPlayer?.displayName ?? 'the current player'} to answer.`
                  : hasSubmitted
                    ? isChoiceTrivia ? 'Choice submitted. Your selection is locked until the result.' : isOpenTrivia ? 'Answer submitted. It is locked until the result.' : 'Submitted. Your guess is locked while you wait for the host.'
                    : hasLocallyExpired
                      ? 'Answering has expired. Waiting for the round result.'
                      : isChoiceTrivia ? 'Choose the correct answer before time runs out.' : isOpenTrivia ? 'Type the correct answer before time runs out.' : 'Enter one answer before time runs out.'}
              </p>
              {isChoiceTrivia ? (
                <div aria-label="Answer choices" className="choice-grid" role="group">
                  {choiceOptions.map((option) => {
                    const selected = option.id === selectedOptionId
                    return <button aria-label={selected ? `${option.label}, selected` : option.label} aria-pressed={selected} className={selected ? 'choice-button choice-button-selected' : 'choice-button'} disabled={hasSubmitted || hasLocallyExpired || isMutating || !activePlayerToken} key={option.id} onClick={() => void handleChoiceSubmit(option.id)} type="button"><span>{option.label}</span>{selected ? <small>Selected</small> : null}</button>
                  })}
                </div>
              ) : <form className="answer-form" onSubmit={handleSubmitGuess}>
                <input
                  aria-label={isOpenTrivia ? 'Your trivia answer' : 'Your guess'}
                  disabled={!isActiveTurn || hasSubmitted || hasLocallyExpired || isMutating || !activePlayerToken}
                  maxLength={120}
                  onChange={(event) => setGuessAnswer(event.target.value)}
                  placeholder={isOpenTrivia ? 'Type your answer' : 'Type your guess'}
                  value={guessAnswer}
                />
                <button className="button button-primary" disabled={!isActiveTurn || hasSubmitted || hasLocallyExpired || isMutating || !guessAnswer.trim()} type="submit">
                  {hasSubmitted ? 'Submitted' : hasLocallyExpired ? 'Time expired' : isMutating ? 'Submitting…' : isOpenTrivia ? 'Submit answer' : 'Submit guess'}
                </button>
              </form>}
              {isSequential && isActiveTurn && !hasSubmitted && !hasLocallyExpired ? (
                <button className="button button-secondary" disabled={isMutating} onClick={() => void handlePassTurn()} type="button">Pass turn</button>
              ) : null}
              {isSequential && currentRound.guesses?.length ? (
                <div className="guess-list">
                  <h2>Prior claims</h2>
                  {currentRound.guesses.map((guess) => <div className="guess-row" key={guess.id}><strong>{guess.playerDisplayName}</strong><span>{guess.rawAnswer}</span></div>)}
                </div>
              ) : null}
            </section>
          ) : null}

          {showRevealState && currentRound && !isGameCompleted && isTrivia ? (
            <section className="participant-phase trivia-result">
              <p className="eyebrow">Round {currentRound.roundIndex} result</p>
              <h1 ref={phaseHeading} tabIndex={-1}>{activeGuess?.correct ? 'Correct!' : 'Not this time'}</h1>
              <dl className="trivia-result-details">
                <div><dt>Correct answer</dt><dd>{isChoiceTrivia ? correctChoiceLabel : currentRound.triviaContent?.canonicalAnswer}</dd></div>
                <div><dt>Your answer</dt><dd>{isChoiceTrivia ? selectedChoiceLabel || 'No answer submitted' : activeGuess?.rawAnswer || 'No answer submitted'}</dd></div>
                <div><dt>Result</dt><dd>{activeGuess?.correct ? 'Correct' : 'Incorrect'} · {activeGuess?.scoreAwarded ?? 0} pts</dd></div>
              </dl>
              {isChoiceTrivia ? <div aria-label="Answer choice results" className="choice-grid choice-results">{choiceOptions.map((option) => {
                const correct = option.id === currentRound.triviaContent?.correctOptionId
                const selected = option.id === revealedSelectedOptionId
                const result = correct ? selected ? 'Correct answer · your choice' : 'Correct answer' : selected ? 'Your choice · incorrect' : 'Incorrect option'
                return <div className={correct ? 'choice-button choice-result-correct' : selected ? 'choice-button choice-result-selected' : 'choice-button'} key={option.id}><span>{option.label}</span><small>{result}</small></div>
              })}</div> : null}
              <div className="score-list participant-score-list" aria-label="Current ranking">
                <h2>Current ranking</h2>
                {rankedScoreboard.map((entry, index) => <div className="score-row" key={entry.playerId}><strong>#{index + 1}</strong><span>{entry.displayName}</span><em>{entry.score} pts</em></div>)}
              </div>
              <p aria-live="polite" className="status-note" role="status">{currentGame?.mode === 'livingroom' ? 'Waiting for the TV to start the next round automatically.' : 'Waiting for the host to start the next round.'}</p>
            </section>
          ) : null}

          {showRevealState && currentRound && !isGameCompleted && !isTrivia ? (
            <section className="participant-phase participant-waiting">
              <p className="eyebrow">Round {currentRound.roundIndex} complete</p>
              <h1 ref={phaseHeading} tabIndex={-1}>Result is on the host display</h1>
              <p aria-live="polite" className="status-note" role="status">
                {currentGame?.mode === 'livingroom' ? 'Waiting for the TV to start the next round automatically.' : 'Waiting for the host to start the next round.'}
              </p>
            </section>
          ) : null}

          {isGameCompleted && currentGame ? (
            <section className="participant-phase">
              <p className="eyebrow">Game complete</p>
              <h1 ref={phaseHeading} tabIndex={-1}>Final scoreboard</h1>
              <div className="score-list participant-score-list">
                {rankedScoreboard.map((entry, index) => {
                  const isWinner = entry.score === winningScore
                  return (
                    <div className={isWinner ? 'score-row score-row-winner' : 'score-row'} key={entry.playerId}>
                      <strong className="score-rank">#{index + 1}</strong>
                      <strong>{entry.displayName}{isWinner ? winnerCount > 1 ? ' — tied winner' : ' — winner' : ''}</strong>
                      <em>{entry.score} pts</em>
                    </div>
                  )
                })}
              </div>
              {currentGame.teamScoreboard?.length ? (
                <div className="score-list participant-score-list">
                  <h2>Final team ranking</h2>
                  {[...currentGame.teamScoreboard].sort((a, b) => b.score - a.score || a.name.localeCompare(b.name)).map((team, index, ranked) => {
                    const winner = team.score === ranked[0]?.score
                    const tied = ranked.filter((entry) => entry.score === ranked[0]?.score).length > 1
                    return <div className={winner ? 'score-row score-row-winner' : 'score-row'} key={team.teamId}><strong>#{index + 1}</strong><span>{team.name}{winner ? tied ? ' — tied team winner' : ' — team winner' : ''}</span><em>{team.score} pts</em></div>
                  })}
                </div>
              ) : null}
              <div className="action-row">
                {currentGame.replayId ? <button className="button button-primary" onClick={() => void handleShareResults()} type="button">Share results</button> : null}
                {currentGame.replayId ? <button className="button button-secondary" onClick={() => navigate(`/replay/${currentGame.replayId}`)} type="button">View replay</button> : null}
                <button className="button button-secondary" onClick={() => navigate('/')} type="button">Back to home</button>
              </div>
              <p aria-atomic="true" aria-live="polite" className="status-note" role="status">{shareStatus}</p>
            </section>
          ) : null}

          {isLoading ? <p aria-live="polite" className="status-note" role="status">Loading room…</p> : null}
          {isMutating ? <p aria-live="polite" className="status-note" role="status">Updating room…</p> : null}
          {errorMessage ? <p className="form-error" role="alert">{errorMessage}</p> : null}
        </article>
      </section>
    )
  }

  return (
    <section className="room-grid">
      <article aria-busy={isLoading || isMutating} className="panel room-overview">
        <div className="section-heading">
          <p className="eyebrow">Room state</p>
          <h1>{room?.name || code}</h1>
          <p>Live events trigger authoritative room refreshes, with polling recovery when the stream is unavailable.</p>
          <p aria-live="polite" className={`connection-indicator connection-${connectionState}`} role="status">{connectionMessage}</p>
        </div>

        <div className="room-code-row"><span>Room code</span><strong>{code}</strong></div>
        <div className="action-row room-tools">
          <button className="button button-secondary" onClick={() => void handleCopyInvite()} type="button">Copy invite</button>
          <button className="button button-secondary" onClick={() => void handleFullscreen()} type="button">Presentation mode</button>
        </div>
        <p aria-atomic="true" aria-live="polite" className="status-note" role="status">{shareStatus} {presentationStatus}</p>
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
            <h2 ref={phaseHeading} tabIndex={-1}>Lobby</h2>
            <p className="info-note">Start when everyone has joined. New players cannot join after the game begins.</p>
            {room?.settings.mode === 'teams' ? (
              <div className="team-setup">
                <p className="info-note">Create 2–4 teams and assign every player. Assignments lock at start; answer claims stay global.</p>
                {isHost ? (
                  <>
                    <form className="answer-form" onSubmit={handleCreateTeam}>
                      <input aria-label="New team name" maxLength={24} onChange={(event) => setTeamName(event.target.value)} placeholder="Team name" value={teamName} />
                      <button className="button button-secondary" disabled={isMutating || !teamName.trim() || (room.teams?.length ?? 0) >= 4} type="submit">Create team</button>
                    </form>
                    {room.players.map((player) => (
                      <label key={player.id}>
                        {player.displayName}
                        <select aria-label={`Team for ${player.displayName}`} disabled={isMutating} onChange={(event) => void handleAssignTeam(player.id, event.target.value)} value={player.teamId ?? ''}>
                          <option value="" disabled>Assign a team</option>
                          {room.teams?.map((team) => <option key={team.id} value={team.id}>{team.name}</option>)}
                        </select>
                      </label>
                    ))}
                  </>
                ) : <p className="status-note">The host is assigning teams.</p>}
              </div>
            ) : null}
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
            <h2 ref={phaseHeading} tabIndex={-1}>{currentRound.question.text}</h2>
            <p className="countdown" role="timer">{hasLocallyExpired ? 'Time expired' : `${secondsRemaining}s remaining`}</p>
            <p aria-atomic="true" aria-live="polite" className="status-note" role="status">
              {isSequential && !isActiveTurn
                ? `Waiting for ${currentTurnPlayer?.displayName ?? 'the current player'} to answer.`
                : hasSubmitted
                ? isChoiceTrivia ? 'Choice submitted. Your selection is locked until reveal.' : isOpenTrivia ? 'Answer submitted. It is locked until reveal.' : 'Submitted. Your guess is locked while you wait for reveal.'
                : hasLocallyExpired
                  ? 'Answering has expired. The server will reveal the round automatically.'
                  : isChoiceTrivia ? 'Choose one answer. Correctness stays hidden until reveal.' : isOpenTrivia ? 'Accepting one answer. Correctness stays hidden until reveal.' : 'Accepting answers. The board stays hidden until reveal.'}
            </p>
            {isChoiceTrivia ? (
              <div aria-label="Answer choices" className="choice-grid" role="group">
                {choiceOptions.map((option) => {
                  const selected = option.id === selectedOptionId
                  return <button aria-pressed={selected} className={selected ? 'choice-button choice-button-selected' : 'choice-button'} disabled={hasSubmitted || hasLocallyExpired || isMutating || !activePlayerToken} key={option.id} onClick={() => void handleChoiceSubmit(option.id)} type="button"><span>{option.label}</span>{selected ? <small>Selected</small> : null}</button>
                })}
              </div>
            ) : <form className="answer-form" onSubmit={handleSubmitGuess}>
              <input
                aria-label={isOpenTrivia ? 'Your trivia answer' : 'Your guess'}
                disabled={!isActiveTurn || hasSubmitted || hasLocallyExpired || isMutating || !activePlayerToken}
                maxLength={120}
                onChange={(event) => setGuessAnswer(event.target.value)}
                placeholder={isOpenTrivia ? 'Type your answer' : 'Type your guess'}
                value={guessAnswer}
              />
              <button className="button button-primary" disabled={!isActiveTurn || hasSubmitted || hasLocallyExpired || isMutating || !guessAnswer.trim()} type="submit">
                {hasSubmitted ? 'Submitted' : hasLocallyExpired ? 'Time expired' : isMutating ? 'Submitting…' : isOpenTrivia ? 'Submit answer' : 'Submit guess'}
              </button>
            </form>}
            {isSequential && isActiveTurn && !hasSubmitted && !hasLocallyExpired ? (
              <button className="button button-secondary" disabled={isMutating} onClick={() => void handlePassTurn()} type="button">Pass turn</button>
            ) : null}
            {isSequential && currentRound.guesses?.length ? (
              <div className="guess-list">
                <h3>Prior claims</h3>
                {currentRound.guesses.map((guess) => <div className="guess-row" key={guess.id}><strong>{guess.playerDisplayName}</strong><span>{guess.rawAnswer}</span></div>)}
              </div>
            ) : null}
            {isHost ? (
              <div className="action-row">
                <button className="button button-secondary" disabled={isMutating} onClick={() => void handleRevealRound()} type="button">Reveal round</button>
              </div>
            ) : null}
          </section>
        ) : null}

        {showRevealState && currentRound ? (
          <section className="phase-card reveal-card">
            <div className="round-meta"><span className="eyebrow">Revealed</span><strong>{isOpenTrivia ? 'Open Trivia result' : isChoiceTrivia ? 'Choice Trivia result' : `Board hash ${currentRound.boardHash}`}</strong></div>
            <h2 ref={phaseHeading} tabIndex={-1}>{currentRound.question.text}</h2>
            <p aria-live="polite" className="status-note" role="status">Answers and awarded scores are now revealed.</p>
            <div className="answer-board">
              {isOpenTrivia && currentRound.triviaContent?.canonicalAnswer ? <div className="answer-row"><span>Correct answer</span><strong>{currentRound.triviaContent.canonicalAnswer}</strong><em>{currentRound.triviaContent.baseScore} pts</em></div> : null}
              {isChoiceTrivia && correctChoiceLabel ? <div className="answer-row"><span>Correct answer</span><strong>{correctChoiceLabel}</strong><em>{currentRound.triviaContent?.baseScore} pts</em></div> : null}
              {currentRound.board?.answers.map((answer) => (
                <div className="answer-row" key={answer.id}><span>#{answer.rank}</span><strong>{answer.canonicalAnswer}</strong><em>{answer.score} pts</em></div>
              ))}
            </div>
            {isChoiceTrivia ? <div aria-label="Answer choice results" className="choice-grid choice-results">{choiceOptions.map((option) => <div className={option.id === currentRound.triviaContent?.correctOptionId ? 'choice-button choice-result-correct' : 'choice-button'} key={option.id}><span>{option.label}</span><small>{option.id === currentRound.triviaContent?.correctOptionId ? 'Correct answer' : 'Incorrect option'}</small></div>)}</div> : null}
            <div className="guess-list">
              <h3>Guesses</h3>
              {currentRound.guesses?.length ? currentRound.guesses.map((guess) => (
                <div className="guess-row" key={guess.id}>
                  <div className="guess-main">
                    <strong>{guess.playerDisplayName}</strong><span>{isChoiceTrivia ? choiceOptions.find((option) => option.id === guess.selectedOptionId)?.label ?? 'Unknown choice' : guess.rawAnswer}</span>
                    <span>{isTrivia ? guess.correct ? 'Correct' : 'Incorrect' : guess.duplicate ? 'Duplicate answer' : guess.matchedPredictionAnswerId ? 'Matched' : 'Miss'}</span>
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
                    {isHost && isTrivia ? (
                      <div className="action-row trivia-correction" aria-label={`Correct ${guess.playerDisplayName}'s result`}>
                        <button className="button button-secondary" disabled={isMutating || guess.correct === true} onClick={() => void handleTriviaOverride(guess.id, true)} type="button">Mark correct</button>
                        <button className="button button-secondary" disabled={isMutating || guess.correct === false} onClick={() => void handleTriviaOverride(guess.id, false)} type="button">Mark incorrect</button>
                      </div>
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
            {isGameCompleted && currentGame?.replayId ? (
              <div className="action-row">
                <button className="button button-primary" onClick={() => void handleShareResults()} type="button">Share results</button>
                <button className="button button-secondary" onClick={() => navigate(`/replay/${currentGame.replayId}`)} type="button">View replay</button>
                {isHost ? <button className="button button-secondary" disabled={isMutating} onClick={() => void handlePlayAgain()} type="button">Play again</button> : null}
                <button className="button button-secondary" onClick={() => navigate('/')} type="button">Back to home</button>
              </div>
            ) : null}
          </section>
        ) : null}

        {isLoading ? <p aria-live="polite" className="status-note" role="status">Loading room…</p> : null}
        {isMutating ? <p aria-live="polite" className="status-note" role="status">Updating room…</p> : null}
        {errorMessage ? <p className="form-error" role="alert">{errorMessage}</p> : null}
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
        {currentGame?.teamScoreboard?.length ? (
          <div className="score-list">
            <div className="section-heading compact-heading"><p className="eyebrow">Teams</p><h2>{isGameCompleted ? 'Final team ranking' : 'Team scores'}</h2></div>
            {[...currentGame.teamScoreboard].sort((a, b) => b.score - a.score || a.name.localeCompare(b.name)).map((team, index, ranked) => {
              const winner = isGameCompleted && team.score === ranked[0]?.score
              return <div className={winner ? 'score-row score-row-winner' : 'score-row'} key={team.teamId}><strong>#{index + 1}</strong><span>{team.name}{winner ? ' — team winner' : ''}</span><em>{team.score} pts</em></div>
            })}
          </div>
        ) : null}
      </aside>
    </section>
  )
}
