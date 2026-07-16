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

  it('propagates a replay 500 without clearing the refreshed session', async () => {
    setAccessToken('expired')
    vi.spyOn(axios, 'post').mockResolvedValue({ data: { access_token: 'fresh' } })
    const onUnauthorized = vi.fn()
    setUnauthorizedHandler(onUnauthorized)
    apiClient.defaults.adapter = async (config) => {
      if (config.headers.Authorization !== 'Bearer fresh') throw unauthorized(config)
      const response: AxiosResponse = { data: {}, status: 500, statusText: 'Server Error', headers: {}, config }
      throw new AxiosError('server error', 'ERR_BAD_RESPONSE', config, undefined, response)
    }

    const result = await Promise.allSettled([apiClient.get('/projects')])

    expect(result[0]?.status).toBe('rejected')
    expect(onUnauthorized).not.toHaveBeenCalled()
    expect((await import('./client')).getAccessToken()).toBe('fresh')
  })
})

describe('API query serialization', () => {
  it('serializes a single array value without square brackets', () => {
    const uri = apiClient.getUri({
      url: '/statistics/realtime',
      params: { point_code: ['android_uv'] },
    })

    expect(uri).toContain('point_code=android_uv')
    expect(uri).not.toContain('point_code%5B%5D')
    expect(uri).not.toContain('point_code[]')
  })

  it('serializes multiple array values as repeated plain keys', () => {
    const uri = apiClient.getUri({
      url: '/diagnostics/recent-events',
      params: { point_code: ['android_uv', 'web_view'] },
    })

    const query = uri.split('?')[1] ?? ''
    const values = new URLSearchParams(query).getAll('point_code')
    expect(values).toEqual(['android_uv', 'web_view'])
  })
})
