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
  const start = zonedBoundary(new Date(end.getTime() - days * 86_400_000), timezone, 'day')
  return { start: start.toISOString(), end: end.toISOString() }
}

function zonedBoundary(value: Date, timezone: string, granularity: 'hour'|'day') {
  const parts = new Intl.DateTimeFormat('en-CA', { timeZone: timezone, year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', hourCycle: 'h23' }).formatToParts(value)
  const get = (type: Intl.DateTimeFormatPartTypes) => Number(parts.find((part) => part.type === type)?.value)
  const desired = Date.UTC(get('year'), get('month') - 1, get('day'), granularity === 'hour' ? get('hour') : 0)
  let candidate = desired
  for (let i = 0; i < 2; i += 1) {
    const at = new Date(candidate)
    const actual = new Intl.DateTimeFormat('en-CA', { timeZone: timezone, year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', hourCycle: 'h23' }).formatToParts(at)
    const part = (type: Intl.DateTimeFormatPartTypes) => Number(actual.find((item) => item.type === type)?.value)
    const represented = Date.UTC(part('year'), part('month') - 1, part('day'), part('hour'))
    candidate += desired - represented
  }
  return new Date(candidate)
}
