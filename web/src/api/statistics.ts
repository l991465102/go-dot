import { apiClient } from './client'

export interface StatisticsQuery {
  projectId: number
  start: string
  end: string
  timezone: string
  granularity: 'hour' | 'day'
  domainCategoryCode?: string
  moduleCategoryCode?: string
  sceneCategoryCode?: string
  pointCodes?: string[]
  platform?: string
  channel?: string
}

export interface StatisticsSummary { pv: number; uv: number; approximate: boolean }
export interface TrendPoint extends StatisticsSummary { period: string }
export interface TrendResponse { items: TrendPoint[] }

function params(query: StatisticsQuery) {
  return {
    project_id: query.projectId, start: query.start, end: query.end, timezone: query.timezone,
    granularity: query.granularity, domain_category_code: query.domainCategoryCode || undefined,
    module_category_code: query.moduleCategoryCode || undefined, scene_category_code: query.sceneCategoryCode || undefined,
    point_code: query.pointCodes, platform: query.platform || undefined, channel: query.channel || undefined,
  }
}

export async function getRealtime(query: StatisticsQuery, signal?: AbortSignal) {
  return (await apiClient.get<StatisticsSummary>('/statistics/realtime', { params: params(query), signal })).data
}
export async function getSummary(query: StatisticsQuery, signal?: AbortSignal) {
  return (await apiClient.get<StatisticsSummary>('/statistics/summary', { params: params(query), signal })).data
}
export async function getTrend(query: StatisticsQuery, signal?: AbortSignal) {
  return (await apiClient.get<TrendResponse>('/statistics/trend', { params: params(query), signal })).data
}

export function completedHourRange(hours: number, timezone: string) {
  const end = zonedBoundary(new Date(), timezone, 'hour')
  return { start: new Date(end.getTime() - hours * 3_600_000).toISOString(), end: end.toISOString() }
}
export function completedDayRange(days: number, timezone: string) {
  const end = zonedBoundary(new Date(), timezone, 'day')
  const endDate = zonedParts(end, timezone)
  const startDate = new Date(Date.UTC(endDate.year, endDate.month - 1, endDate.day - days))
  const start = resolveZonedBoundary(Date.UTC(startDate.getUTCFullYear(), startDate.getUTCMonth(), startDate.getUTCDate()), timezone)
  return { start: start.toISOString(), end: end.toISOString() }
}

function zonedBoundary(value: Date, timezone: string, granularity: 'hour'|'day') {
  const parts = zonedParts(value, timezone)
  const desired = Date.UTC(parts.year, parts.month - 1, parts.day, granularity === 'hour' ? parts.hour : 0)
  return resolveZonedBoundary(desired, timezone)
}

function zonedParts(value: Date, timezone: string) {
  const parts = new Intl.DateTimeFormat('en-CA', { timeZone: timezone, year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', hourCycle: 'h23' }).formatToParts(value)
  const get = (type: Intl.DateTimeFormatPartTypes) => Number(parts.find((part) => part.type === type)?.value)
  return { year: get('year'), month: get('month'), day: get('day'), hour: get('hour') }
}

function resolveZonedBoundary(desired: number, timezone: string) {
  let candidate = desired
  for (let i = 0; i < 2; i += 1) {
    const actual = zonedParts(new Date(candidate), timezone)
    const represented = Date.UTC(actual.year, actual.month - 1, actual.day, actual.hour)
    candidate += desired - represented
  }
  return new Date(candidate)
}
