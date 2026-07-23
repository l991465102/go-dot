import type { DailyResult, ReportPoint } from '@/api/reports'

export interface WideTableColumn {
  key: string
  label: string
}

export interface WideTableRow {
  date: string
  [key: string]: string | number
}

export interface DailyWideTable {
  columns: WideTableColumn[]
  rows: WideTableRow[]
}

function metricKey(pointId: number, metric: 'pv' | 'uv'): string {
  return `point_${pointId}_${metric}`
}

function descendingDates(start: string, end: string): string[] {
  const startAt = Date.parse(`${start}T00:00:00Z`)
  const endAt = Date.parse(`${end}T00:00:00Z`)
  if (!Number.isFinite(startAt) || !Number.isFinite(endAt) || startAt > endAt) return []

  const dates: string[] = []
  for (let current = endAt; current >= startAt; current -= 86_400_000) {
    dates.push(new Date(current).toISOString().slice(0, 10))
  }
  return dates
}

export function buildDailyWideTable(result: DailyResult, points: readonly ReportPoint[]): DailyWideTable {
  const columns = points.flatMap((point) => [
    { key: metricKey(point.id, 'pv'), label: `${point.code}_pv` },
    { key: metricKey(point.id, 'uv'), label: `${point.code}_uv` },
  ])
  const rowsByDate = new Map<string, WideTableRow>()

  for (const date of descendingDates(result.start_date, result.end_date)) {
    const row: WideTableRow = { date }
    for (const point of points) {
      row[metricKey(point.id, 'pv')] = 0
      row[metricKey(point.id, 'uv')] = 0
    }
    rowsByDate.set(date, row)
  }

  for (const item of result.items) {
    const row = rowsByDate.get(item.date)
    if (!row) continue
    row[metricKey(item.point_id, 'pv')] = item.pv
    row[metricKey(item.point_id, 'uv')] = item.uv
  }

  return { columns, rows: [...rowsByDate.values()] }
}
