import { FormEvent, useState } from 'react'
import { useNavigate } from 'react-router-dom'

import { createRoom, type GameKind } from '../lib/api'
import { saveSession } from '../lib/session'

export function CreateRoomPage() {
    const navigate = useNavigate()
    const [roomName, setRoomName] = useState('Friday Night')
    const [hostDisplayName, setHostDisplayName] = useState('Host')
    const [totalRounds, setTotalRounds] = useState(5)
    const [answerTimerSeconds, setAnswerTimerSeconds] = useState(45)
    const [locale, setLocale] = useState('en')
    const [predictionModel, setPredictionModel] = useState('gpt-5.6-luna')
    const [teamSafeMode, setTeamSafeMode] = useState(false)
    const [mode, setMode] = useState<'simultaneous' | 'teams' | 'sequential' | 'livingroom'>('simultaneous')
    const [gameKind, setGameKind] = useState<GameKind>('model_says')
    const [isSubmitting, setIsSubmitting] = useState(false)
    const [errorMessage, setErrorMessage] = useState<string | null>(null)

    async function handleSubmit(event: FormEvent<HTMLFormElement>) {
        event.preventDefault()
        setIsSubmitting(true)
        setErrorMessage(null)

        try {
            const response = await createRoom({
                roomName,
                hostDisplayName,
                settings: {
                    mode,
                    gameKind,
                    totalRounds,
                    answerTimerSeconds,
                    locale,
                    predictionModel,
                    teamSafeMode,
                },
            })

            if (response.player) {
                saveSession(response.room.code, response.player)
            }

            navigate(`/room/${response.room.code}`)
        } catch (error) {
            setErrorMessage(error instanceof Error ? error.message : 'Unable to create room')
        } finally {
            setIsSubmitting(false)
        }
    }

    return (
        <section className="panel form-panel">
            <div className="section-heading">
                <p className="eyebrow">Host setup</p>
                <h1>Create a room</h1>
                <p>Choose phone play, teams, turns, or a non-playing living-room TV host.</p>
            </div>

            <form className="form-grid" onSubmit={handleSubmit}>
                <fieldset className="ruleset-picker">
                    <legend>Game rules</legend>
                    <label><input checked={gameKind === 'model_says'} name="game-kind" onChange={() => setGameKind('model_says')} type="radio" /> <span><strong>Model Says</strong><small>Guess the answers the model ranked.</small></span></label>
                    <label><input checked={gameKind === 'trivia_open'} name="game-kind" onChange={() => { setGameKind('trivia_open'); if (mode === 'sequential') setMode('simultaneous') }} type="radio" /> <span><strong>Open Trivia</strong><small>Type the single correct answer. Accepted spelling variants count.</small></span></label>
                    <label><input checked={gameKind === 'trivia_choice'} name="game-kind" onChange={() => { setGameKind('trivia_choice'); if (mode === 'sequential') setMode('simultaneous') }} type="radio" /> <span><strong>Choice Trivia</strong><small>Pick the one correct answer from four options.</small></span></label>
                </fieldset>
                <label>
                    Pacing
                    <select value={mode} onChange={(event) => setMode(event.target.value as 'simultaneous' | 'teams' | 'sequential' | 'livingroom')}>
                        <option value="simultaneous">Individual</option>
                        <option value="teams">Teams</option>
                        <option disabled={gameKind !== 'model_says'} value="sequential">Sequential turns{gameKind !== 'model_says' ? ' (Model Says only)' : ''}</option>
                        <option value="livingroom">Living-room TV host</option>
                    </select>
                </label>
                <label>
                    Room name
                    <input maxLength={48} value={roomName} onChange={(event) => setRoomName(event.target.value)} required />
                </label>

                <label>
                    {mode === 'livingroom' ? 'TV display name' : 'Host display name'}
                    <input
                        value={hostDisplayName}
                        maxLength={24}
                        onChange={(event) => setHostDisplayName(event.target.value)}
                        required
                    />
                </label>

                <label>
                    Rounds
                    <input
                        type="number"
                        min={1}
                        max={5}
                        value={totalRounds}
                        onChange={(event) => setTotalRounds(Number(event.target.value))}
                    />
                </label>

                <label>
                    Answer timer
                    <input
                        type="number"
                        min={15}
                        max={120}
                        value={answerTimerSeconds}
                        onChange={(event) => setAnswerTimerSeconds(Number(event.target.value))}
                    />
                </label>

                <label>
                    Locale
                    <select value={locale} onChange={(event) => setLocale(event.target.value)}>
                        <option value="en">English</option>
                        <option value="ro">Română</option>
                    </select>
                </label>

                <label>
                    Prediction model
                    <input
                        value={predictionModel}
                        onChange={(event) => setPredictionModel(event.target.value)}
                    />
                </label>

                <label className="checkbox-row">
                    <input
                        type="checkbox"
                        checked={teamSafeMode}
                        onChange={(event) => setTeamSafeMode(event.target.checked)}
                    />
                    Team-building safe mode
                </label>

                {errorMessage ? <p className="form-error" role="alert">{errorMessage}</p> : null}

                <button className="button button-primary" disabled={isSubmitting} type="submit">
                    {isSubmitting ? 'Creating room…' : 'Create room'}
                </button>
            </form>
        </section>
    )
}
