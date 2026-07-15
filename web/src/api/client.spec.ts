import axios, { AxiosError, type AxiosResponse, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { apiClient, setAccessToken, setUnauthorizedHandler } from './client'

const originalAdapter = apiClient.defaults.adapter

function unauthorized(config: InternalAxiosRequestConfig) {
  const response: AxiosResponse = { data: {}, status: 401, statusText: 'Unauthorized', headers: {}, config }
  return new AxiosError('unauthorized', 'ERR_BAD_REQUEST', config, undefined, response)
}

describe('API authentication recovery', () => {
  afterEach(() => {
    apiClient.defaults.adapter = originalAdapter
    setUnauthorizedHandler(null)
    setAccessToken(null)
    vi.restoreAllMocks()
  })

  it('uses one refresh request for concurrent 401 responses', async () => {
    setAccessToken('expired')
    vi.spyOn(axios, 'post').mockResolvedValue({ data: { access_token: 'fresh' } })
    apiClient.defaults.adapter = async (config) => {
      if (config.headers.Authorization !== 'Bearer fresh') throw unauthorized(config)
      return { data: { ok: true }, status: 200, statusText: 'OK', headers: {}, config }
    }

    const responses = await Promise.all([apiClient.get('/projects'), apiClient.get('/projects')])

    expect(responses.map(({ data }) => data)).toEqual([{ ok: true }, { ok: true }])
    expect(axios.post).toHaveBeenCalledTimes(1)
  })

  it('performs one unauthorized transition when a shared refresh fails', async () => {
    setAccessToken('expired')
    vi.spyOn(axios, 'post').mockRejectedValue(new Error('refresh expired'))
    apiClient.defaults.adapter = async (config) => { throw unauthorized(config) }
    const onUnauthorized = vi.fn()
    setUnauthorizedHandler(onUnauthorized)

    await Promise.allSettled([apiClient.get('/projects'), apiClient.get('/projects')])

    expect(onUnauthorized).toHaveBeenCalledTimes(1)
  })
})
