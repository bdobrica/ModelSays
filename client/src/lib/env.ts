const fallbackApiBaseUrl =
  typeof window === 'undefined'
    ? 'http://localhost:8080'
    : `${window.location.protocol}//${window.location.hostname}:8080`

export const env = {
  apiBaseUrl: import.meta.env.VITE_API_BASE_URL?.trim() || fallbackApiBaseUrl,
}
