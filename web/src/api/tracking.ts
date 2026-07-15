import { apiClient } from './client'

export type CategoryLevel = 'DOMAIN' | 'MODULE' | 'SCENE'
export type DefinitionState = 'DRAFT' | 'PUBLISHED' | 'DEPRECATED'
export type DevelopmentState = 'PENDING' | 'DEVELOPING' | 'TESTING' | 'ONLINE'
export type BehaviorType = 'PAGE_VIEW' | 'ELEMENT_EXPOSE' | 'ELEMENT_CLICK' | 'BUSINESS_ACTION' | 'BUSINESS_RESULT' | 'CUSTOM'
export type PropertyType = 'STRING' | 'INTEGER' | 'DECIMAL' | 'BOOLEAN' | 'DATETIME' | 'ENUM' | 'ARRAY'

export interface Category { id: number; project_id: number; parent_id?: number; code: string; name: string; level: CategoryLevel; description?: string; sort_order: number; active: boolean; created_by: number }
export interface CategoryInput { parent_id?: number; code: string; name: string; level: CategoryLevel; description?: string; sort_order: number }
export interface CategoryUpdate { name: string; description?: string; sort_order: number; active: boolean }
export interface PointProperty { code: string; name: string; type: PropertyType | ''; required: boolean; enum_values?: string[]; array_item_type?: Exclude<PropertyType, 'ENUM' | 'ARRAY'>; example_value?: unknown; description?: string; sort_order: number }
export interface Point {
  id: number; project_id: number; category_id: number; category: Category; category_path: Array<Pick<Category, 'id'|'code'|'name'|'level'>>;
  code: string; name: string; description?: string; behavior_type: BehaviorType | ''; definition_state: DefinitionState; development_state: DevelopmentState;
  trigger_page?: string; trigger_timing?: string; trigger_count_rule?: string; abnormal_trigger_rule?: string; count_pv: boolean; count_uv: boolean;
  uv_mode: 'APPROXIMATE'|'EXACT'; time_basis: 'CLIENT'|'SERVER'; supported_platforms: string[]; supported_channels: string[];
  product_owner?: string; developer_owner?: string; requirement_url?: string; properties: PointProperty[]; published_version?: number;
  deprecation_reason?: string; replacement_point_id?: number; created_by: number; updated_by: number;
}
export type PointDraft = Omit<Point, 'id'|'project_id'|'category'|'category_path'|'definition_state'|'created_by'|'updated_by'|'published_version'|'deprecation_reason'|'replacement_point_id'>
export interface PointFilters { q?: string; category_id?: number; definition_state?: DefinitionState|''; development_state?: DevelopmentState|''; behavior_type?: BehaviorType|''; platform?: string; owner?: string; created_from?: string; created_to?: string; page?: number; page_size?: number }
export interface PointList { items: Point[]; total: number }

export async function listCategories(projectId: number, signal?: AbortSignal) { return (await apiClient.get<Category[]>(`/projects/${projectId}/categories`, { signal })).data }
export async function createCategory(projectId: number, input: CategoryInput) { return (await apiClient.post<Category>(`/projects/${projectId}/categories`, input)).data }
export async function updateCategory(projectId: number, id: number, input: CategoryUpdate) { return (await apiClient.patch<Category>(`/projects/${projectId}/categories/${id}`, input)).data }
export async function listPoints(projectId: number, params: PointFilters, signal?: AbortSignal) { return (await apiClient.get<PointList>(`/projects/${projectId}/points`, { params, signal })).data }
export async function getPoint(projectId: number, id: number, signal?: AbortSignal) { return (await apiClient.get<Point>(`/projects/${projectId}/points/${id}`, { signal })).data }
export async function createPoint(projectId: number, input: PointDraft) { return (await apiClient.post<Point>(`/projects/${projectId}/points`, input)).data }
export async function updateDraft(projectId: number, id: number, input: PointDraft) { return (await apiClient.patch<Point>(`/projects/${projectId}/points/${id}`, input)).data }
export async function publishPoint(projectId: number, id: number) { return (await apiClient.post(`/projects/${projectId}/points/${id}/publish`)).data }
export async function copyPoint(projectId: number, id: number, code: string) { return (await apiClient.post<Point>(`/projects/${projectId}/points/${id}/copy`, { code })).data }
export async function deprecatePoint(projectId: number, id: number, reason: string, replacementId?: number) { await apiClient.post(`/projects/${projectId}/points/${id}/deprecate`, { reason, replacement_id: replacementId }) }
export async function updateDevelopmentState(projectId: number, id: number, state: DevelopmentState) { return (await apiClient.patch<Point>(`/projects/${projectId}/points/${id}/development-state`, { state })).data }
