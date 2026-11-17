import { apiClient } from './client'
export type ChartKind = 'TREND'|'FUNNEL'
export interface ChartConfig { point_codes:string[]; start:string; end:string; window_hours?:number; metric?:'pv'|'uv'; granularity?:'hour'|'day'; timezone?:string; platform?:string; channel?:string; domain?:string; module?:string; scene?:string }
export interface SavedChart { id:number; project_id:number; name:string; kind:ChartKind; config:ChartConfig; created_by:number; created_at:string }
export async function listCharts(project:number,kind:ChartKind){return (await apiClient.get<SavedChart[]>(`/projects/${project}/charts`,{params:{kind}})).data}
export async function createChart(project:number,kind:ChartKind,name:string,config:ChartConfig){return (await apiClient.post<{id:number}>(`/projects/${project}/charts`,{name,kind,config})).data}
export async function deleteChart(project:number,id:number){await apiClient.delete(`/projects/${project}/charts/${id}`)}
