import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'

import { getReplay, type ReplaySummary } from '../lib/api'

export function ReplayPage() {
  const { replayId = '' } = useParams()
  const [replay, setReplay] = useState<ReplaySummary | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    const controller = new AbortController()
    void getReplay(replayId, controller.signal)
      .then((response) => setReplay(response.replay))
      .catch((reason) => {
        if (!controller.signal.aborted) setError(reason instanceof Error ? reason.message : 'Replay unavailable')
      })
    return () => controller.abort()
  }, [replayId])

  if (error) {
    return (
      <section className="panel phase-card">
        <p className="eyebrow">Game replay</p>
        <h1>Replay unavailable</h1>
        <p className="form-error" role="alert">{error}</p>
        <Link className="button button-primary" to="/">Back to home</Link>
      </section>
    )
  }
  if (!replay) return <p className="status-note" role="status">Loading replay…</p>

  const topScore = replay.rankings[0]?.score
  const winnerCount = replay.rankings.filter((entry) => entry.score === topScore).length
  return (
    <section className="room-grid">
      <article className="panel room-overview">
        <p className="eyebrow">Completed game replay</p>
        <h1>{replay.roomName}</h1>
        {replay.rounds.map((round) => (
          <section className="phase-card" key={round.roundIndex}>
            <p className="eyebrow">Round {round.roundIndex}</p>
            <h2>{round.question}</h2>
            <div className="answer-board">
              {round.board.map((answer) => {
                const matches = round.guesses.filter((guess) => guess.matchedPredictionAnswerId === answer.id)
                return (
                  <div className="answer-row" key={answer.id}>
                    <span>#{answer.rank}</span><strong>{answer.canonicalAnswer}</strong><em>{answer.score} pts</em>
                    <small>{matches.length ? `Matched by ${matches.map((guess) => guess.playerDisplayName).join(', ')}` : 'No match'}</small>
                  </div>
                )
              })}
            </div>
            <div className="guess-list">
              <h3>Revealed guesses and round deltas</h3>
              {round.guesses.length ? round.guesses.map((guess, index) => (
                <div className="guess-row" key={`${guess.playerDisplayName}-${index}`}>
                  <span><strong>{guess.playerDisplayName}</strong>: {guess.rawAnswer}</span>
                  <em>{guess.scoreAwarded > 0 ? '+' : ''}{guess.scoreAwarded} pts{guess.duplicate ? ' — duplicate' : ''}</em>
                </div>
              )) : <p>No guesses were submitted.</p>}
              {round.scoreDeltas.map((entry) => (
                <div className="score-row" key={entry.playerId}>
                  <span>{entry.displayName}</span>
                  <em>{entry.score > 0 ? '+' : ''}{entry.score} pts this round</em>
                </div>
              ))}
            </div>
          </section>
        ))}
        <Link className="button button-primary" to="/">Back to home</Link>
      </article>
      <aside className="panel player-panel">
        <h2>Final rankings</h2>
        {replay.rankings.map((entry, index) => (
          <div className={entry.score === topScore ? 'score-row score-row-winner' : 'score-row'} key={entry.playerId}>
            <strong>#{index + 1}</strong>
            <span>{entry.displayName}{entry.score === topScore ? winnerCount > 1 ? ' — tied winner' : ' — winner' : ''}</span>
            <em>{entry.score} pts</em>
          </div>
        ))}
      </aside>
    </section>
  )
}
