import { FormEvent, useState } from 'react'
import { useNavigate } from 'react-router-dom'

import { createRoom } from '../lib/api'
import { saveSession } from '../lib/session'

export function CreateRoomPage() {
    const navigate = useNavigate()
    const [roomName, setRoomName] = useState('Friday Night')
    const [hostDisplayName, setHostDisplayName] = useState('Host')
    const [totalRounds, setTotalRounds] = useState(5)
    const [answerTimerSeconds, setAnswerTimerSeconds] = useState(45)
    const [locale, setLocale] = useState('en')
    const [predictionModel, setPredictionModel] = useState('gpt-4.1-mini')
    const [teamSafeMode, setTeamSafeMode] = useState(false)
    const [mode, setMode] = useState<'simultaneous' | 'teams' | 'sequential'>('simultaneous')
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
                <p>Choose individual competition or auditable team competition with individual guesses.</p>
            </div>

            <form className="form-grid" onSubmit={handleSubmit}>
                <label>
                    Game mode
                    <select value={mode} onChange={(event) => setMode(event.target.value as 'simultaneous' | 'teams' | 'sequential')}>
                        <option value="simultaneous">Individual</option>
                        <option value="teams">Teams</option>
                        <option value="sequential">Sequential turns</option>
                    </select>
                </label>
                <label>
                    Room name
                    <input maxLength={48} value={roomName} onChange={(event) => setRoomName(event.target.value)} required />
                </label>

                <label>
                    Host display name
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
                    <input value={locale} onChange={(event) => setLocale(event.target.value)} />
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

                {errorMessage ? <p className="form-error">{errorMessage}</p> : null}

                <button className="button button-primary" disabled={isSubmitting} type="submit">
                    {isSubmitting ? 'Creating room…' : 'Create room'}
                </button>
            </form>
        </section>
    )
}
