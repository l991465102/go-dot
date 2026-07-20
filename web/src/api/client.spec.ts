import axios, { AxiosError, AxiosHeaders, type AxiosResponse, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { apiClient, setAccessToken, setUnauthorizedHandler } from './client'
import {
  createReport,
  deleteReport,
  exportDaily,
  getReport,
  listReports,
  queryDaily,
  updateReport,
  type DailyExport,
  type ReportMutation,
  type SavedReport,
} from './reports'

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

describe('report API client', () => {
  afterEach(() => {
    apiClient.defaults.adapter = originalAdapter
    vi.restoreAllMocks()
  })

  it('uses project-scoped endpoints for saved-report CRUD', async () => {
    const requests: InternalAxiosRequestConfig[] = []
    const report: SavedReport = {
      id: 7,
      project_id: 9,
      name: '核心日报',
      description: '核心点位',
      default_days: 30,
      points: [],
      created_by: 1,
      updated_by: 1,
      created_at: '2026-07-20T01:00:00Z',
      updated_at: '2026-07-20T02:00:00Z',
    }
    apiClient.defaults.adapter = async (config) => {
      requests.push(config)
      return {
        data: config.method === 'delete' ? undefined : config.method === 'get' && config.url === '/projects/9/reports' ? [report] : report,
        status: config.method === 'delete' ? 204 : 200,
        statusText: 'OK',
        headers: {},
        config,
      }
    }
    const input: ReportMutation = { name: '核心日报', description: '核心点位', default_days: 30, point_ids: [3, 2] }

    await expect(listReports(9)).resolves.toEqual([report])
    await expect(getReport(9, 7)).resolves.toEqual(report)
    await expect(createReport(9, input)).resolves.toEqual(report)
    await expect(updateReport(9, 7, { ...input, expected_updated_at: report.updated_at })).resolves.toEqual(report)
    await expect(deleteReport(9, 7)).resolves.toBeUndefined()

    expect(requests.map(({ method, url }) => [method, url])).toEqual([
      ['get', '/projects/9/reports'],
      ['get', '/projects/9/reports/7'],
      ['post', '/projects/9/reports'],
      ['patch', '/projects/9/reports/7'],
      ['delete', '/projects/9/reports/7'],
    ])
    expect(JSON.parse(String(requests[2]?.data))).toEqual(input)
    expect(JSON.parse(String(requests[3]?.data))).toEqual({ ...input, expected_updated_at: report.updated_at })
  })

  it('posts the daily query and forwards its abort signal', async () => {
    const controller = new AbortController()
    let request: InternalAxiosRequestConfig | undefined
    apiClient.defaults.adapter = async (config) => {
      request = config
      return {
        data: { timezone: 'Asia/Shanghai', start_date: '2026-07-20', end_date: '2026-07-20', point_count: 1, day_count: 1, total_pv: 12, items: [] },
        status: 200,
        statusText: 'OK',
        headers: {},
        config,
      }
    }

    await queryDaily({ project_id: 9, start_date: '2026-07-20', end_date: '2026-07-20', point_ids: [3] }, controller.signal)

    expect(request?.url).toBe('/statistics/daily-by-point/query')
    expect(request?.method).toBe('post')
    expect(request?.signal).toBe(controller.signal)
  })

  it.each([
    [`attachment; filename="fallback.xlsx"; filename*="UTF-8''%E6%A0%B8%E5%BF%83%E6%97%A5%E6%8A%A5.xlsx"`, '核心日报.xlsx'],
    [`attachment; filename=fallback.xlsx; filename*=UTF-8''%E6%A0%B8%E5%BF%83.xlsx`, '核心.xlsx'],
    [`attachment; filename=fallback.xlsx; filename*=UTF-8'zh-CN'%E6%A0%B8%E5%BF%83.xlsx`, '核心.xlsx'],
    [`attachment; filename=fallback.xlsx; filename*=UTF-8''not%ZZvalid`, 'custom-report.xlsx'],
    [`attachment; filename=fallback.xlsx; filename*=UTF-8''..%2Fsecret.xlsx`, 'custom-report.xlsx'],
  ])('exports an authenticated blob and safely parses %s', async (disposition, expectedFilename) => {
    const blob = new Blob(['xlsx'])
    let request: InternalAxiosRequestConfig | undefined
    apiClient.defaults.adapter = async (config) => {
      request = config
      return { data: blob, status: 200, statusText: 'OK', headers: { 'content-disposition': disposition }, config }
    }
    const input: DailyExport = {
      project_id: 9,
      start_date: '2026-07-20',
      end_date: '2026-07-20',
      point_ids: [3],
      report_name: '核心日报',
      report_description: '',
    }

    await expect(exportDaily(input)).resolves.toEqual({ blob, filename: expectedFilename })
    expect(request?.url).toBe('/statistics/daily-by-point/export')
    expect(request?.method).toBe('post')
    expect(request?.responseType).toBe('blob')
  })

  it('reads Content-Disposition case-insensitively from AxiosHeaders', async () => {
    const blob = new Blob(['xlsx'])
    apiClient.defaults.adapter = async (config) => ({
      data: blob,
      status: 200,
      statusText: 'OK',
      headers: new AxiosHeaders({ 'Content-Disposition': `attachment; filename*=UTF-8''axios.xlsx` }),
      config,
    })

    const result = await exportDaily({
      project_id: 9,
      start_date: '2026-07-20',
      end_date: '2026-07-20',
      point_ids: [3],
      report_name: '',
      report_description: '',
    })

    expect(result).toEqual({ blob, filename: 'axios.xlsx' })
  })

  it('falls back to a case-insensitive lookup for plain-object response headers', async () => {
    const blob = new Blob(['xlsx'])
    vi.spyOn(apiClient, 'post').mockResolvedValue({
      data: blob,
      headers: { 'Content-Disposition': `attachment; filename*=UTF-8''raw.xlsx` },
    } as never)

    const result = await exportDaily({
      project_id: 9,
      start_date: '2026-07-20',
      end_date: '2026-07-20',
      point_ids: [3],
      report_name: '',
      report_description: '',
    })

    expect(result).toEqual({ blob, filename: 'raw.xlsx' })
  })
})
