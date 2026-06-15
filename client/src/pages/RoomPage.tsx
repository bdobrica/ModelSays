import { FormEvent, useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'

import { getRoom, nextRound, overrideMatch, revealRound, startGame, submitGuess, type Room } from '../lib/api'
import { loadSession } from '../lib/session'

export function RoomPage() {
    const { code = '' } = useParams()
    const [room, setRoom] = useState<Room | null>(null)
    const [isLoading, setIsLoading] = useState(true)
    const [isMutating, setIsMutating] = useState(false)
    const [errorMessage, setErrorMessage] = useState<string | null>(null)
    const [guessAnswer, setGuessAnswer] = useState('')
    const [overrideSelections, setOverrideSelections] = useState<Record<string, string>>({})
    const session = loadSession()
    const activePlayer = session?.roomCode === code ? session.player : null
    const activePlayerId = activePlayer?.id ?? null
    const activePlayerToken = activePlayer?.token ?? ''
    const isHost = activePlayer?.isHost ?? false
    const currentGame = room?.currentGame
    const currentRound = currentGame?.currentRound
    const activeScore = currentGame?.scoreboard.find((entry) => entry.playerId === activePlayerId)
    const hasSubmitted = activeScore?.submissionMade ?? false
    const isGameCompleted = currentGame?.status === 'completed'

    async function refreshRoom(showLoading = true) {
        if (showLoading) {
            setIsLoading(true)
        }
        setErrorMessage(null)

        try {
            const response = await getRoom(code)
            setRoom(response.room)
        } catch (error) {
            setErrorMessage(error instanceof Error ? error.message : 'Unable to load room')
        } finally {
            if (showLoading) {
                setIsLoading(false)
            }
        }
    }

    useEffect(() => {
        let isMounted = true

        async function loadRoom() {
            await refreshRoom()
        }

        void loadRoom()

        return () => {
            isMounted = false
        }
    }, [code])

    useEffect(() => {
        setGuessAnswer('')
        setOverrideSelections({})
    }, [currentRound?.id])

    useEffect(() => {
        const intervalId = window.setInterval(() => {
            if (!isMutating) {
                void refreshRoom(false)
            }
        }, 3000)

        return () => {
            window.clearInterval(intervalId)
        }
    }, [code, isMutating])

    async function mutateRoom(action: () => Promise<Room>) {
        setIsMutating(true)
        setErrorMessage(null)

        try {
            const updatedRoom = await action()
            setRoom(updatedRoom)
        } catch (error) {
            setErrorMessage(error instanceof Error ? error.message : 'Action failed')
        } finally {
            setIsMutating(false)
        }
    }

    async function handleStartGame() {
        if (!activePlayerToken) {
            setErrorMessage('Missing player session token')
            return
        }

        await mutateRoom(async () => {
            const response = await startGame(code, { playerToken: activePlayerToken })
            return response.room
        })
    }

    async function handleSubmitGuess(event: FormEvent<HTMLFormElement>) {
        event.preventDefault()
        if (!activePlayerToken || !currentRound) {
            setErrorMessage('Missing player session or round state')
            return
        }

        await mutateRoom(async () => {
            const response = await submitGuess(code, currentRound.id, {
                playerToken: activePlayerToken,
                answer: guessAnswer,
            })
            return response.room
        })
    }

    async function handleRevealRound() {
        if (!activePlayerToken || !currentRound) {
            setErrorMessage('Missing player session or round state')
            return
        }

        await mutateRoom(async () => {
            const response = await revealRound(code, currentRound.id, { playerToken: activePlayerToken })
            return response.room
        })
    }

    async function handleNextRound() {
        if (!activePlayerToken) {
            setErrorMessage('Missing player session token')
            return
        }

        await mutateRoom(async () => {
            const response = await nextRound(code, { playerToken: activePlayerToken })
            return response.room
        })
    }

    async function handleOverrideMatch(guessId: string) {
        if (!activePlayerToken || !currentRound) {
            setErrorMessage('Missing player session or round state')
            return
        }

        const selectedValue = overrideSelections[guessId] ?? ''
        await mutateRoom(async () => {
            const response = await overrideMatch(code, {
                playerToken: activePlayerToken,
                roundId: currentRound.id,
                guessId,
                matchedPredictionAnswerId: selectedValue || null,
            })
            return response.room
        })
    }

    const showLobbyState = room != null && !currentGame
    const showAnswerState = currentGame != null && currentRound != null && currentRound.status === 'answering'
    const showRevealState = currentGame != null && currentRound != null && currentRound.status === 'revealed'

    return (
        <section className="room-grid">
            <article className="panel room-overview">
                <div className="section-heading">
                    <p className="eyebrow">Room state</p>
                    <h1>{room?.name || code}</h1>
                    <p>Backend-backed lobby data is live now. WebSocket sync and round state come next.</p>
                </div>

                <div className="room-code-row">
                    <span>Room code</span>
                    <strong>{code}</strong>
                </div>

                <div className="settings-grid">
                    <div>
                        <span>Status</span>
                        <strong>{room?.status || 'loading'}</strong>
                    </div>
                    <div>
                        <span>Mode</span>
                        <strong>{room?.settings.mode || 'simultaneous'}</strong>
                    </div>
                    <div>
                        <span>Rounds</span>
                        <strong>{room?.settings.totalRounds ?? '—'}</strong>
                    </div>
                    <div>
                        <span>Timer</span>
                        <strong>{room ? `${room.settings.answerTimerSeconds}s` : '—'}</strong>
                    </div>
                    <div>
                        <span>Locale</span>
                        <strong>{room?.settings.locale || '—'}</strong>
                    </div>
                    <div>
                        <span>Model</span>
                        <strong>{room?.settings.predictionModel || '—'}</strong>
                    </div>
                </div>

                <button
                    className="button button-secondary"
                    onClick={() => {
                        void refreshRoom(false)
                    }}
                    type="button"
                >
                    Refresh room state
                </button>

                {showLobbyState ? (
                    <section className="phase-card">
                        <h2>Lobby</h2>
                        <p className="info-note">Everyone is in. Start the game to freeze the first board and begin round one.</p>
                        {isHost ? (
                            <button className="button button-primary" disabled={isMutating || !activePlayerToken} onClick={() => void handleStartGame()} type="button">
                                {isMutating ? 'Starting…' : 'Start game'}
                            </button>
                        ) : (
                            <p className="status-note">Waiting for the host to start the game.</p>
                        )}
                    </section>
                ) : null}

                {showAnswerState && currentRound ? (
                    <section className="phase-card">
                        <div className="round-meta">
                            <span className="eyebrow">Round {currentRound.roundIndex}</span>
                            <strong>{currentGame?.totalRounds ? `${currentRound.roundIndex}/${currentGame.totalRounds}` : currentRound.roundIndex}</strong>
                        </div>
                        <h2>{currentRound.question.text}</h2>
                        <p className="info-note">Board frozen: {currentRound.boardHash}</p>
                        <p className="status-note">The answer board is hidden until reveal. Submit one guess.</p>

                        <form className="answer-form" onSubmit={handleSubmitGuess}>
                            <input
                                disabled={hasSubmitted || isMutating || !activePlayerToken}
                                onChange={(event) => setGuessAnswer(event.target.value)}
                                placeholder="Type your guess"
                                value={guessAnswer}
                            />
                            <button className="button button-primary" disabled={hasSubmitted || isMutating || !guessAnswer.trim()} type="submit">
                                {hasSubmitted ? 'Submitted' : isMutating ? 'Submitting…' : 'Submit guess'}
                            </button>
                        </form>

                        {hasSubmitted ? <p className="info-note">Your guess is locked for this round.</p> : null}

                        <div className="action-row">
                            {isHost ? (
                                <button className="button button-secondary" disabled={isMutating || !activePlayerToken} onClick={() => void handleRevealRound()} type="button">
                                    Reveal round
                                </button>
                            ) : null}
                        </div>
                    </section>
                ) : null}

                {showRevealState && currentRound ? (
                    <section className="phase-card">
                        <div className="round-meta">
                            <span className="eyebrow">Revealed</span>
                            <strong>Board hash {currentRound.boardHash}</strong>
                        </div>
                        <h2>{currentRound.question.text}</h2>

                        <div className="answer-board">
                            {currentRound.board?.answers.map((answer) => (
                                <div className="answer-row" key={answer.id}>
                                    <span>#{answer.rank}</span>
                                    <strong>{answer.canonicalAnswer}</strong>
                                    <em>{answer.score} pts</em>
                                </div>
                            ))}
                        </div>

                        <div className="guess-list">
                            <h3>Guesses</h3>
                            {currentRound.guesses?.length ? (
                                currentRound.guesses.map((guess) => (
                                    <div className="guess-row" key={guess.id}>
                                        <div className="guess-main">
                                            <strong>{guess.playerDisplayName}</strong>
                                            <span>{guess.rawAnswer}</span>
                                            <span>{guess.duplicate ? 'Duplicate answer' : guess.matchedPredictionAnswerId ? 'Matched' : 'Miss'}</span>
                                        </div>
                                        <div className="guess-actions">
                                            <em>{guess.scoreAwarded} pts</em>
                                            {isHost && currentRound.board ? (
                                                <>
                                                    <select
                                                        className="override-select"
                                                        onChange={(event) => {
                                                            setOverrideSelections((current) => ({
                                                                ...current,
                                                                [guess.id]: event.target.value,
                                                            }))
                                                        }}
                                                        value={overrideSelections[guess.id] ?? guess.matchedPredictionAnswerId ?? ''}
                                                    >
                                                        <option value="">Mark as miss</option>
                                                        {currentRound.board.answers.map((answer) => (
                                                            <option key={answer.id} value={answer.id}>
                                                                {answer.rank}. {answer.canonicalAnswer}
                                                            </option>
                                                        ))}
                                                    </select>
                                                    <button className="button button-secondary" disabled={isMutating || !activePlayerToken} onClick={() => void handleOverrideMatch(guess.id)} type="button">
                                                        Apply override
                                                    </button>
                                                </>
                                            ) : null}
                                        </div>
                                    </div>
                                ))
                            ) : (
                                <p className="status-note">No guesses were submitted this round.</p>
                            )}
                        </div>

                        {isGameCompleted ? <p className="info-note">Game complete. Final scoreboard below.</p> : null}

                        <div className="action-row">
                            {isHost && !isGameCompleted ? (
                                <button className="button button-primary" disabled={isMutating || !activePlayerToken} onClick={() => void handleNextRound()} type="button">
                                    {currentGame && currentGame.currentRoundIndex >= currentGame.totalRounds ? 'Finish game' : 'Next round'}
                                </button>
                            ) : null}
                        </div>
                    </section>
                ) : null}

                {isLoading ? <p className="status-note">Loading room…</p> : null}
                {isMutating ? <p className="status-note">Updating room…</p> : null}
                {errorMessage ? <p className="form-error">{errorMessage}</p> : null}
            </article>

            <aside className="panel player-panel">
                <div className="section-heading compact-heading">
                    <p className="eyebrow">Players</p>
                    <h2>{room?.players.length ?? 0} in lobby</h2>
                </div>

                <ul className="player-list">
                    {room?.players.map((player) => (
                        <li key={player.id} className={player.id === activePlayerId ? 'player-card player-card-active' : 'player-card'}>
                            <div>
                                <strong>{player.displayName}</strong>
                                <span>{player.isHost ? 'Host' : 'Player'}</span>
                            </div>
                            {player.id === activePlayerId ? <em>You</em> : null}
                        </li>
                    ))}
                </ul>

                {currentGame ? (
                    <div className="score-list">
                        <div className="section-heading compact-heading">
                            <p className="eyebrow">Scores</p>
                            <h2>{isGameCompleted ? 'Final scoreboard' : 'Live scoreboard'}</h2>
                        </div>

                        {currentGame.scoreboard.map((entry) => (
                            <div className="score-row" key={entry.playerId}>
                                <div>
                                    <strong>{entry.displayName}</strong>
                                    <span>{entry.submissionMade ? 'Submitted' : 'Waiting'}</span>
                                </div>
                                <em>{entry.score} pts</em>
                            </div>
                        ))}
                    </div>
                ) : null}
            </aside>
        </section>
    )
}
