import { Link } from 'react-router-dom'

export function HomePage() {
    return (
        <section className="hero-grid">
            <div className="hero-copy panel panel-hero">
                <p className="eyebrow">Party game MVP</p>
                <h1>What would the model say?</h1>
                <p className="lede">
                    Model Says is Family Feud for AI cultural priors. The board locks before anyone answers.
                    Players are not guessing survey results. They are guessing the model.
                </p>

                <div className="hero-actions">
                    <Link className="button button-primary" to="/create">
                        Create a room
                    </Link>
                    <Link className="button button-secondary" to="/join">
                        Join by code
                    </Link>
                </div>
            </div>

            <div className="panel panel-stack">
                <div className="feature-card">
                    <span className="feature-step">1</span>
                    <h2>Freeze the board</h2>
                    <p>The backend generates one shared answer board and keeps it stable for the round.</p>
                </div>
                <div className="feature-card">
                    <span className="feature-step">2</span>
                    <h2>Collect guesses</h2>
                    <p>Players submit a single answer before the timer closes.</p>
                </div>
                <div className="feature-card">
                    <span className="feature-step">3</span>
                    <h2>Reveal the AI</h2>
                    <p>Scores come from matching the board, not from what people think is objectively true.</p>
                </div>
            </div>
        </section>
    )
}
