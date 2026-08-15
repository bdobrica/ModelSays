import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import * as api from '../lib/api'
import { CreateRoomPage } from './CreateRoomPage'

vi.mock('../lib/api', async () => {
  const actual = await vi.importActual<typeof import('../lib/api')>('../lib/api')
  return { ...actual, createRoom: vi.fn(), getPublicConfig: vi.fn() }
})

describe('CreateRoomPage', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(api.getPublicConfig).mockResolvedValue({ availableLocales: ['en', 'ro'] })
  })

  it('keeps rules and pacing distinct and creates Open Trivia rooms', async () => {
    vi.mocked(api.createRoom).mockResolvedValue({
      room: { code: 'ABC234' } as api.Room,
      player: { id: 'host', displayName: 'Host', isHost: true, joinedAt: '', token: 'token' },
    })
    render(<MemoryRouter initialEntries={['/create']}><Routes><Route path="/create" element={<CreateRoomPage />} /><Route path="/room/:code" element={<p>Created</p>} /></Routes></MemoryRouter>)

    expect(screen.getByRole('group', { name: 'Game rules' })).toBeInTheDocument()
    expect(screen.getByLabelText(/Choice Trivia/)).toBeEnabled()
    fireEvent.click(screen.getByLabelText(/Open Trivia/))
    expect(screen.getByRole('combobox', { name: 'Pacing' })).toHaveValue('simultaneous')
    expect(screen.getByRole('option', { name: /Sequential turns/ })).toBeDisabled()
    fireEvent.click(screen.getByRole('button', { name: 'Create room' }))

    await waitFor(() => expect(api.createRoom).toHaveBeenCalledWith(expect.objectContaining({
      settings: expect.objectContaining({ gameKind: 'trivia_open', mode: 'simultaneous' }),
    })))
    expect(await screen.findByText('Created')).toBeInTheDocument()
  })

  it('creates Choice Trivia independently from pacing', async () => {
    vi.mocked(api.createRoom).mockResolvedValue({ room: { code: 'ABC234' } as api.Room, player: { id: 'host', displayName: 'Host', isHost: true, joinedAt: '', token: 'token' } })
    render(<MemoryRouter initialEntries={['/create']}><Routes><Route path="/create" element={<CreateRoomPage />} /><Route path="/room/:code" element={<p>Created</p>} /></Routes></MemoryRouter>)

    fireEvent.click(screen.getByLabelText(/Choice Trivia/))
    fireEvent.click(screen.getByRole('button', { name: 'Create room' }))
    await waitFor(() => expect(api.createRoom).toHaveBeenCalledWith(expect.objectContaining({ settings: expect.objectContaining({ gameKind: 'trivia_choice', mode: 'simultaneous' }) })))
  })

  it('renders locales supplied by server configuration', async () => {
    vi.mocked(api.getPublicConfig).mockResolvedValue({ availableLocales: ['ro', 'de'] })
    render(<MemoryRouter><CreateRoomPage /></MemoryRouter>)
    const locale = screen.getByRole('combobox', { name: 'Locale' })
    await waitFor(() => expect(locale).toHaveValue('ro'))
    expect(screen.getByRole('option', { name: 'RO' })).toBeInTheDocument()
    expect(screen.getByRole('option', { name: 'DE' })).toBeInTheDocument()
    expect(screen.queryByRole('option', { name: 'EN' })).not.toBeInTheDocument()
  })
})
