import { Route, Routes } from 'react-router-dom'

import { AppShell } from './components/AppShell'
import { CreateRoomPage } from './pages/CreateRoomPage'
import { HomePage } from './pages/HomePage'
import { JoinRoomPage } from './pages/JoinRoomPage'
import { NotFoundPage } from './pages/NotFoundPage'
import { RoomPage } from './pages/RoomPage'
import { ReplayPage } from './pages/ReplayPage'

export default function App() {
    return (
        <AppShell>
            <Routes>
                <Route path="/" element={<HomePage />} />
                <Route path="/create" element={<CreateRoomPage />} />
                <Route path="/join" element={<JoinRoomPage />} />
                <Route path="/room/:code" element={<RoomPage />} />
                <Route path="/replay/:replayId" element={<ReplayPage />} />
                <Route path="*" element={<NotFoundPage />} />
            </Routes>
        </AppShell>
    )
}
