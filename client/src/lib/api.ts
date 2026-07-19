import { env } from './env'

export type GameMode = 'simultaneous'

export interface RoomSettings {
  mode: GameMode
  totalRounds: number
  answerTimerSeconds: number
  locale: string
  predictionModel: string
  teamSafeMode: boolean
}

export interface Player {
  id: string
  displayName: string
  isHost: boolean
  joinedAt: string
  token?: string
}

export type GameStatus = 'in_progress' | 'completed'
export type RoundStatus = 'answering' | 'revealed'

export interface PredictionAnswer {
  id: string
  canonicalAnswer: string
  aliases: string[]
  rank: number
  score: number
  createdAt: string
}

export interface PredictionBoard {
  id: string
  provider: string
  modelName: string
  promptVersion: string
  boardHash: string
  answers: PredictionAnswer[]
  createdAt: string
}

export interface Guess {
  id: string
  playerId: string
  playerDisplayName: string
  rawAnswer: string
  normalizedAnswer: string
  matchedPredictionAnswerId?: string
  scoreAwarded: number
  duplicate: boolean
  createdAt: string
}

export interface ScoreboardEntry {
  playerId: string
  displayName: string
  score: number
  isHost: boolean
  submissionMade: boolean
}

export interface Question {
  id: string
  text: string
  locale: string
  category: string
  createdAt: string
}

export interface Round {
  id: string
  roundIndex: number
  status: RoundStatus
  question: Question
  boardHash: string
  board?: PredictionBoard
  guesses?: Guess[]
  answerPhaseStartedAt: string
  answerPhaseEndsAt: string
  revealStartedAt?: string
  createdAt: string
}

export interface Game {
  id: string
  status: GameStatus
  mode: GameMode
  totalRounds: number
  currentRoundIndex: number
  currentRound?: Round
  scoreboard: ScoreboardEntry[]
  createdAt: string
  startedAt: string
  endedAt?: string
}

export interface Room {
  code: string
  name: string
  status: string
  settings: RoomSettings
  players: Player[]
  currentGame?: Game
  createdAt: string
  updatedAt: string
}

export interface RoomResponse {
  room: Room
  player?: Player
}

export interface CreateRoomPayload {
  roomName: string
  hostDisplayName: string
  settings: RoomSettings
}

export interface JoinRoomPayload {
  displayName: string
}

export interface PlayerTokenPayload {
  playerToken: string
}

export interface SubmitGuessPayload extends PlayerTokenPayload {
  answer: string
}

export interface OverrideMatchPayload extends PlayerTokenPayload {
  roundId: string
  guessId: string
  matchedPredictionAnswerId: string | null
  judgeSuggestionId?: string
}

export interface JudgeSuggestion {
  id: string
  guessId: string
  suggestedPredictionAnswerId?: string
  confidence: number
  confidenceBand: 'none' | 'low' | 'medium' | 'high'
  rationaleCategory: string
  model: string
  promptVersion: string
  outcome: string
  reviewedAt?: string
  reviewDecision?: string
}

interface ApiErrorPayload {
  error?: string
}

export async function createRoom(payload: CreateRoomPayload): Promise<RoomResponse> {
  return request<RoomResponse>('/api/rooms', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export async function joinRoom(code: string, payload: JoinRoomPayload): Promise<RoomResponse> {
  return request<RoomResponse>(`/api/rooms/${code}/join`, {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export async function getRoom(code: string, signal?: AbortSignal): Promise<RoomResponse> {
  return request<RoomResponse>(`/api/rooms/${code}`, { signal })
}

export async function recoverSession(code: string, payload: PlayerTokenPayload, signal?: AbortSignal): Promise<RoomResponse> {
  return request<RoomResponse>(`/api/rooms/${code}/session`, {
    method: 'POST',
    body: JSON.stringify(payload),
    signal,
  })
}

export async function startGame(code: string, payload: PlayerTokenPayload): Promise<RoomResponse> {
  return request<RoomResponse>(`/api/rooms/${code}/start`, {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export async function submitGuess(code: string, roundId: string, payload: SubmitGuessPayload): Promise<RoomResponse> {
  return request<RoomResponse>(`/api/rooms/${code}/rounds/${roundId}/guesses`, {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export async function revealRound(code: string, roundId: string, payload: PlayerTokenPayload): Promise<RoomResponse> {
  return request<RoomResponse>(`/api/rooms/${code}/rounds/${roundId}/reveal`, {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export async function nextRound(code: string, payload: PlayerTokenPayload): Promise<RoomResponse> {
  return request<RoomResponse>(`/api/rooms/${code}/next-round`, {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export async function overrideMatch(code: string, payload: OverrideMatchPayload): Promise<RoomResponse> {
  return request<RoomResponse>(`/api/rooms/${code}/override-match`, {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export async function getJudgeSuggestions(code: string, roundId: string, playerToken: string): Promise<{ suggestions: JudgeSuggestion[] }> {
  return request<{ suggestions: JudgeSuggestion[] }>(`/api/rooms/${code}/rounds/${roundId}/judge-suggestions`, {
    headers: { 'X-Player-Token': playerToken },
  })
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(`${env.apiBaseUrl}${path}`, {
    ...init,
    headers: {
      'Content-Type': 'application/json',
      ...(init?.headers ?? {}),
    },
  })

  if (!response.ok) {
    const payload = (await response.json().catch(() => null)) as ApiErrorPayload | null
    throw new Error(payload?.error || 'Request failed')
  }

  return (await response.json()) as T
}
