import { describe, expect, it } from 'vitest'
import type { DailyResult, ReportPoint } from '@/api/reports'
import { buildDailyWideTable } from './report-wide-table'

describe('buildDailyWideTable', () => {
  it('pivots daily point items into ordered metric columns and descending dates', () => {
    const points: ReportPoint[] = [
      { id: 10, code: 'dot_10010', name: '点位 10010', definition_state: 'PUBLISHED', uv_mode: 'EXACT', sort_no: 0 },
      { id: 86, code: 'dot_10086', name: '点位 10086', definition_state: 'PUBLISHED', uv_mode: 'APPROXIMATE', sort_no: 1 },
    ]
    const result: DailyResult = {
      timezone: 'Asia/Shanghai',
      start_date: '2026-07-10',
      end_date: '2026-07-12',
      point_count: 2,
      day_count: 3,
      total_pv: 22,
      items: [
        { date: '2026-07-10', complete: true, point_id: 10, point_code: 'dot_10010', point_name: '点位 10010', pv: 10, uv: 5, uv_mode: 'EXACT' },
        { date: '2026-07-12', complete: true, point_id: 10, point_code: 'dot_10010', point_name: '点位 10010', pv: 6, uv: 4, uv_mode: 'EXACT' },
        { date: '2026-07-11', complete: true, point_id: 10, point_code: 'dot_10010', point_name: '点位 10010', pv: 6, uv: 4, uv_mode: 'EXACT' },
      ],
    }

    expect(buildDailyWideTable(result, points)).toEqual({
      columns: [
        { key: 'point_10_pv', label: 'dot_10010_pv' },
        { key: 'point_10_uv', label: 'dot_10010_uv' },
        { key: 'point_86_pv', label: 'dot_10086_pv' },
        { key: 'point_86_uv', label: 'dot_10086_uv' },
      ],
      rows: [
        { date: '2026-07-12', point_10_pv: 6, point_10_uv: 4, point_86_pv: 0, point_86_uv: 0 },
        { date: '2026-07-11', point_10_pv: 6, point_10_uv: 4, point_86_pv: 0, point_86_uv: 0 },
        { date: '2026-07-10', point_10_pv: 10, point_10_uv: 5, point_86_pv: 0, point_86_uv: 0 },
      ],
    })
  })
})
