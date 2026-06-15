import type { PropsWithChildren } from 'react'
import { NavLink } from 'react-router-dom'

export function AppShell({ children }: PropsWithChildren) {
    return (
        <div className="app-shell">
            <div className="aurora aurora-left" />
            <div className="aurora aurora-right" />
            <header className="topbar">
                <NavLink className="brand" to="/">
                    <span className="brand-mark">MS</span>
                    <span>
                        <strong>Model Says</strong>
                        <small>Guess what the AI thinks people would say.</small>
                    </span>
                </NavLink>

                <nav className="nav-links">
                    <NavLink to="/create">Create room</NavLink>
                    <NavLink to="/join">Join room</NavLink>
                </nav>
            </header>

            <main className="page-frame">{children}</main>
        </div>
    )
}
