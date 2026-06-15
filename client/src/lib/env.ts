const fallbackApiBaseUrl = 'http://localhost:8080'

export const env = {
  apiBaseUrl: import.meta.env.VITE_API_BASE_URL?.trim() || fallbackApiBaseUrl,
  wsUrl: import.meta.env.VITE_WS_URL?.trim() || 'ws://localhost:8080/ws',
}
