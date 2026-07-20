import { apiClient } from './client'

export type ReportPointState = 'DRAFT' | 'PUBLISHED' | 'DEPRECATED'
export type UVMode = 'APPROXIMATE' | 'EXACT'

export interface ReportPoint {
  id: number
  code: string
  name: string
  definition_state: ReportPointState
  uv_mode: UVMode
  sort_no: number
}

export interface SavedReport {
  id: number
  project_id: number
  name: string
  description: string
  default_days: number
  points?: ReportPoint[]
  created_by: number
  updated_by: number
  created_at: string
  updated_at: string
}

export interface ReportMutation {
  name: string
  description: string
  default_days: number
  point_ids: number[]
}

export interface DailyQuery {
  project_id: number
  start_date: string
  end_date: string
  point_ids: number[]
}

export interface DailyExport extends DailyQuery {
  report_name: string
  report_description: string
}

export interface DailyItem {
  date: string
  complete: boolean
  point_id: number
  point_code: string
  point_name: string
  pv: number
  uv: number
  uv_mode: UVMode
}

export interface DailyResult {
  timezone: string
  start_date: string
  end_date: string
  point_count: number
  day_count: number
  total_pv: number
  items: DailyItem[]
}

export async function listReports(projectId: number, signal?: AbortSignal): Promise<SavedReport[]> {
  return (await apiClient.get<SavedReport[]>(`/projects/${projectId}/reports`, { signal })).data
}

export async function getReport(projectId: number, reportId: number, signal?: AbortSignal): Promise<SavedReport> {
  return (await apiClient.get<SavedReport>(`/projects/${projectId}/reports/${reportId}`, { signal })).data
}

export async function createReport(projectId: number, input: ReportMutation): Promise<SavedReport> {
  return (await apiClient.post<SavedReport>(`/projects/${projectId}/reports`, input)).data
}

export async function updateReport(
  projectId: number,
  reportId: number,
  input: ReportMutation & { expected_updated_at: string },
): Promise<SavedReport> {
  return (await apiClient.patch<SavedReport>(`/projects/${projectId}/reports/${reportId}`, input)).data
}

export async function deleteReport(projectId: number, reportId: number): Promise<void> {
  await apiClient.delete(`/projects/${projectId}/reports/${reportId}`)
}

export async function queryDaily(input: DailyQuery, signal?: AbortSignal): Promise<DailyResult> {
  return (await apiClient.post<DailyResult>('/statistics/daily-by-point/query', input, { signal })).data
}

const fallbackFilename = 'custom-report.xlsx'

function filenameFromDisposition(disposition: string | undefined): string {
  if (!disposition) return fallbackFilename
  const match = disposition.match(/(?:^|;)\s*filename\*\s*=\s*(?:"([^"]*)"|([^;]*))/i)
  const value = (match?.[1] ?? match?.[2] ?? '').trim()
  const encoded = /^UTF-8''(.+)$/i.exec(value)?.[1]
  if (!encoded) return fallbackFilename
  try {
    const filename = decodeURIComponent(encoded).trim()
    if (
      !filename
      || filename.length > 255
      || !/\.xlsx$/i.test(filename)
      || /[\u0000-\u001f\u007f/\\]/.test(filename)
      || filename === '.'
      || filename === '..'
    ) return fallbackFilename
    return filename
  } catch {
    return fallbackFilename
  }
}

export async function exportDaily(input: DailyExport): Promise<{ blob: Blob; filename: string }> {
  const response = await apiClient.post<Blob>('/statistics/daily-by-point/export', input, { responseType: 'blob' })
  return {
    blob: response.data,
    filename: filenameFromDisposition(response.headers['content-disposition']),
  }
}
