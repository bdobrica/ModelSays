import { Link } from 'react-router-dom'

export function NotFoundPage() {
    return (
        <section className="panel form-panel narrow-panel">
            <div className="section-heading">
                <p className="eyebrow">404</p>
                <h1>That route does not exist yet.</h1>
                <p>Use the home screen to create a room, join a room, or reopen a known room code.</p>
            </div>

            <Link className="button button-primary" to="/">
                Back home
            </Link>
        </section>
    )
}
