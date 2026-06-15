import { FormEvent, useState } from 'react'
import { useNavigate } from 'react-router-dom'

import { joinRoom } from '../lib/api'
import { saveSession } from '../lib/session'

export function JoinRoomPage() {
    const navigate = useNavigate()
    const [roomCode, setRoomCode] = useState('')
    const [displayName, setDisplayName] = useState('')
    const [isSubmitting, setIsSubmitting] = useState(false)
    const [errorMessage, setErrorMessage] = useState<string | null>(null)

    async function handleSubmit(event: FormEvent<HTMLFormElement>) {
        event.preventDefault()
        setIsSubmitting(true)
        setErrorMessage(null)

        try {
            const response = await joinRoom(roomCode.trim().toUpperCase(), { displayName })
            if (response.player) {
                saveSession(response.room.code, response.player)
            }
            navigate(`/room/${response.room.code}`)
        } catch (error) {
            setErrorMessage(error instanceof Error ? error.message : 'Unable to join room')
        } finally {
            setIsSubmitting(false)
        }
    }

    return (
        <section className="panel form-panel narrow-panel">
            <div className="section-heading">
                <p className="eyebrow">Player entry</p>
                <h1>Join a room</h1>
                <p>Players can already join against the backend in-memory room service.</p>
            </div>

            <form className="form-grid" onSubmit={handleSubmit}>
                <label>
                    Room code
                    <input
                        value={roomCode}
                        onChange={(event) => setRoomCode(event.target.value.toUpperCase())}
                        maxLength={6}
                        required
                    />
                </label>

                <label>
                    Display name
                    <input
                        value={displayName}
                        onChange={(event) => setDisplayName(event.target.value)}
                        required
                    />
                </label>

                {errorMessage ? <p className="form-error">{errorMessage}</p> : null}

                <button className="button button-primary" disabled={isSubmitting} type="submit">
                    {isSubmitting ? 'Joining…' : 'Join room'}
                </button>
            </form>
        </section>
    )
}
