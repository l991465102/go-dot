import axios, { AxiosError, type InternalAxiosRequestConfig } from 'axios'

interface RetryableRequest extends InternalAxiosRequestConfig {
  _authRetried?: boolean
}

interface RefreshPayload {
  access_token: string
}

let accessToken: string | null = null
let refreshPromise: Promise<string> | null = null
let unauthorizedHandler: (() => void) | null = null
let unauthorizedNotified = false

export const apiClient = axios.create({ baseURL: '/api/v1', withCredentials: true })

export function setAccessToken(token: string | null) {
  accessToken = token
  if (token) unauthorizedNotified = false
}

export function getAccessToken() {
  return accessToken
}

export function setUnauthorizedHandler(handler: (() => void) | null) {
  unauthorizedHandler = handler
}

async function refreshAccessToken() {
  if (!refreshPromise) {
    refreshPromise = axios
      .post<RefreshPayload>('/api/v1/auth/refresh', undefined, { withCredentials: true })
      .then(({ data }) => {
        setAccessToken(data.access_token)
        return data.access_token
      })
      .finally(() => {
        refreshPromise = null
      })
  }
  return refreshPromise
}

apiClient.interceptors.request.use((config) => {
  if (accessToken) config.headers.Authorization = `Bearer ${accessToken}`
  return config
})

apiClient.interceptors.response.use(
  (response) => response,
  async (error: AxiosError) => {
    const request = error.config as RetryableRequest | undefined
    if (error.response?.status !== 401 || !request || request._authRetried || request.url === '/auth/refresh') {
      throw error
    }
    request._authRetried = true
    try {
      const token = await refreshAccessToken()
      request.headers.Authorization = `Bearer ${token}`
      return await apiClient(request)
    } catch (refreshError) {
      setAccessToken(null)
      if (!unauthorizedNotified) {
        unauthorizedNotified = true
        unauthorizedHandler?.()
      }
      throw refreshError
    }
  },
)
