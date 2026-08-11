import { env } from './env'

export type GameMode = 'simultaneous' | 'teams' | 'sequential' | 'livingroom'
export type GameKind = 'model_says' | 'trivia_open' | 'trivia_choice'

export interface RoomSettings {
  mode: GameMode
  gameKind: GameKind
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
  role?: 'participant' | 'host_display'
  joinedAt: string
  token?: string
  teamId?: string
}

export interface Team { id: string; name: string }

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

export interface TeamScoreboardEntry { teamId: string; name: string; score: number }

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
  revealPhaseEndsAt?: string
  createdAt: string
  turnOrder?: string[]
  currentTurnIndex?: number
  turnEndsAt?: string
}

export interface Game {
  id: string
  replayId?: string
  status: GameStatus
  mode: GameMode
  gameKind: GameKind
  totalRounds: number
  currentRoundIndex: number
  currentRound?: Round
  scoreboard: ScoreboardEntry[]
  teamScoreboard?: TeamScoreboardEntry[]
  createdAt: string
  startedAt: string
  endedAt?: string
}

export interface ReplayAnswer {
  id: string
  canonicalAnswer: string
  rank: number
  score: number
}

export interface ReplayGuess {
  playerDisplayName: string
  rawAnswer: string
  matchedPredictionAnswerId?: string
  scoreAwarded: number
  duplicate: boolean
}

export interface ReplayRound {
  roundIndex: number
  question: string
  board: ReplayAnswer[]
  guesses: ReplayGuess[]
  scoreDeltas: ScoreboardEntry[]
}

export interface ReplaySummary {
  id: string
  roomName: string
  mode: GameMode
  gameKind: GameKind
  startedAt: string
  endedAt: string
  rankings: ScoreboardEntry[]
  teamRankings?: TeamScoreboardEntry[]
  teams?: Team[]
  rounds: ReplayRound[]
}

export interface Room {
  code: string
  revision: number
  name: string
  status: string
  settings: RoomSettings
  players: Player[]
  teams?: Team[]
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

export async function passTurn(code: string, roundId: string, payload: PlayerTokenPayload): Promise<RoomResponse> {
  return request<RoomResponse>(`/api/rooms/${code}/rounds/${roundId}/pass`, {
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

export async function playAgain(code: string, payload: PlayerTokenPayload): Promise<RoomResponse> {
  return request<RoomResponse>(`/api/rooms/${code}/play-again`, {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export async function createTeam(code: string, payload: PlayerTokenPayload & { name: string }): Promise<RoomResponse> {
  return request<RoomResponse>(`/api/rooms/${code}/teams`, { method: 'POST', body: JSON.stringify(payload) })
}

export async function assignTeam(code: string, playerId: string, payload: PlayerTokenPayload & { teamId: string }): Promise<RoomResponse> {
  return request<RoomResponse>(`/api/rooms/${code}/players/${playerId}/team`, { method: 'POST', body: JSON.stringify(payload) })
}

export async function getReplay(replayId: string, signal?: AbortSignal): Promise<{ replay: ReplaySummary }> {
  return request<{ replay: ReplaySummary }>(`/api/replays/${replayId}`, { signal })
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
