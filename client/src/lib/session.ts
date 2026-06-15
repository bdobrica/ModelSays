import type { Player } from './api'

const storageKey = 'modelsays.session'

interface StoredSession {
  roomCode: string
  player: Player
}

export function saveSession(roomCode: string, player: Player) {
  const session: StoredSession = {
    roomCode,
    player,
  }

  window.localStorage.setItem(storageKey, JSON.stringify(session))
}

export function loadSession(): StoredSession | null {
  const raw = window.localStorage.getItem(storageKey)
  if (!raw) {
    return null
  }

  try {
    return JSON.parse(raw) as StoredSession
  } catch {
    window.localStorage.removeItem(storageKey)
    return null
  }
}
