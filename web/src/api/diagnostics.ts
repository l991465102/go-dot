import { apiClient } from './client'

export interface DiagnosticQuery { projectId: number; start: string; end: string; pointCodes?: string[]; platform?: string; channel?: string; errorCode?: string; cursor?: string; limit?: number }
export interface RecentEvent { event_id: string; point_code: string; platform: string; channel?: string; app_version?: string; client_event_at: string; received_at: string; statistic_at: string; properties?: Record<string, unknown> }
export interface InvalidEvent { event_id?: string; point_code?: string; platform?: string; channel?: string; error_code: string; error_message: string; received_at: string }
export interface DeadLetter { id: string; batch_id?: string; deliveries: number; first_seen: string; last_error?: string }
export interface CursorPage<T> { items: T[]; next_cursor?: string }

function params(query: DiagnosticQuery) { return { project_id: query.projectId, start: query.start, end: query.end, point_code: query.pointCodes, platform: query.platform || undefined, channel: query.channel || undefined, error_code: query.errorCode || undefined, cursor: query.cursor || undefined, limit: query.limit ?? 20 } }
export async function listRecentEvents(query: DiagnosticQuery, signal?: AbortSignal) { return (await apiClient.get<CursorPage<RecentEvent>>('/diagnostics/recent-events', { params: params(query), signal })).data }
export async function listInvalidEvents(query: DiagnosticQuery, signal?: AbortSignal) { return (await apiClient.get<CursorPage<InvalidEvent>>('/diagnostics/invalid-events', { params: params(query), signal })).data }
export async function listDeadLetters(projectId: number, cursor = '', limit = 20, signal?: AbortSignal) { return (await apiClient.get<CursorPage<DeadLetter>>('/diagnostics/dead-letters', { params: { project_id: projectId, cursor: cursor || undefined, limit }, signal })).data }
