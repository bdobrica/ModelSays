import { render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import * as api from '../lib/api'
import { ReplayPage } from './ReplayPage'

vi.mock('../lib/api', async () => ({
  ...(await vi.importActual<typeof import('../lib/api')>('../lib/api')),
  getReplay: vi.fn(),
}))

function renderReplay() {
  return render(
    <MemoryRouter initialEntries={['/replay/replay-id']}>
      <Routes><Route path="/replay/:replayId" element={<ReplayPage />} /></Routes>
    </MemoryRouter>,
  )
}

describe('ReplayPage', () => {
  beforeEach(() => vi.resetAllMocks())

  it('shows tied winners, round matches, guesses, and deltas', async () => {
    vi.mocked(api.getReplay).mockResolvedValue({ replay: {
      id: 'replay-id', roomName: 'Friday', mode: 'simultaneous', gameKind: 'model_says',
      startedAt: '2026-07-19T10:00:00Z', endedAt: '2026-07-19T10:05:00Z',
      rankings: [
        { playerId: 'one', displayName: 'Ana', score: 50, isHost: false, submissionMade: false },
        { playerId: 'two', displayName: 'Bo', score: 50, isHost: true, submissionMade: false },
      ],
      rounds: [{
        roundIndex: 1, question: 'Name a fruit',
        board: [{ id: 'apple', canonicalAnswer: 'Apple', rank: 1, score: 50 }],
        guesses: [{ playerDisplayName: 'Ana', rawAnswer: 'Apple', matchedPredictionAnswerId: 'apple', scoreAwarded: 50, duplicate: false }],
        scoreDeltas: [
          { playerId: 'one', displayName: 'Ana', score: 50, isHost: false, submissionMade: false },
          { playerId: 'two', displayName: 'Bo', score: 0, isHost: true, submissionMade: false },
        ],
      }],
    } })
    renderReplay()
    expect(await screen.findByText('Ana — tied winner')).toBeInTheDocument()
    expect(screen.getByText('Bo — tied winner')).toBeInTheDocument()
    expect(screen.getByText('Matched by Ana')).toBeInTheDocument()
    expect(screen.getByText('+50 pts')).toBeInTheDocument()
    expect(screen.getByText('+50 pts this round')).toBeInTheDocument()
  })

  it('explains an unavailable or expired replay', async () => {
    vi.mocked(api.getReplay).mockRejectedValue(new Error('replay is unavailable or has expired'))
    renderReplay()
    expect(await screen.findByRole('heading', { name: 'Replay unavailable' })).toBeInTheDocument()
    expect(screen.getByRole('alert')).toHaveTextContent('expired')
  })
})
