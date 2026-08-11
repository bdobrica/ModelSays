import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import * as api from '../lib/api'
import { CreateRoomPage } from './CreateRoomPage'

vi.mock('../lib/api', async () => {
  const actual = await vi.importActual<typeof import('../lib/api')>('../lib/api')
  return { ...actual, createRoom: vi.fn() }
})

describe('CreateRoomPage', () => {
  beforeEach(() => vi.resetAllMocks())

  it('keeps rules and pacing distinct and creates Open Trivia rooms', async () => {
    vi.mocked(api.createRoom).mockResolvedValue({
      room: { code: 'ABC234' } as api.Room,
      player: { id: 'host', displayName: 'Host', isHost: true, joinedAt: '', token: 'token' },
    })
    render(<MemoryRouter initialEntries={['/create']}><Routes><Route path="/create" element={<CreateRoomPage />} /><Route path="/room/:code" element={<p>Created</p>} /></Routes></MemoryRouter>)

    expect(screen.getByRole('group', { name: 'Game rules' })).toBeInTheDocument()
    expect(screen.getByLabelText(/Four-choice Trivia/)).toBeDisabled()
    fireEvent.click(screen.getByLabelText(/Open Trivia/))
    expect(screen.getByRole('combobox', { name: 'Pacing' })).toHaveValue('simultaneous')
    expect(screen.getByRole('option', { name: /Sequential turns/ })).toBeDisabled()
    fireEvent.click(screen.getByRole('button', { name: 'Create room' }))

    await waitFor(() => expect(api.createRoom).toHaveBeenCalledWith(expect.objectContaining({
      settings: expect.objectContaining({ gameKind: 'trivia_open', mode: 'simultaneous' }),
    })))
    expect(await screen.findByText('Created')).toBeInTheDocument()
  })
})
