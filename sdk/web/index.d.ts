export interface Event { event_id?: string; point_code: string; event_time?: number; user_id?: string; anonymous_id?: string; session_id?: string; platform: 'web'|'ios'|'android'|'mini_program'|'server'; app_version?: string; channel?: string; properties?: Record<string, unknown> }
export interface EventResult { index: number; event_id: string; status: 'accepted'|'rejected'; code?: string; message?: string }
export interface Options { baseURL: string; writeKey: string; test?: boolean; maxRetries?: number; retryBackoff?: number; maxQueueSize?: number; fetchImpl?: typeof fetch }
export function uuidv7(): string
export class Client { constructor(options: Options); track(event: Event): string; pending(): Event[]; flush(options?: { signal?: AbortSignal }): Promise<EventResult[]> }
