import { apiClient } from './client'

export type ProjectRole = 'ADMIN' | 'VIEWER'

interface ApiProject {
  id: number
  code: string
  name: string
  allowed_origins: string[] | null
  timezone: string
  is_active: boolean
  created_by: number
  role: ProjectRole
}

export interface Project {
  id: number
  code: string
  name: string
  allowedOrigins: string[]
  timezone: string
  active: boolean
  createdBy: number
  role: ProjectRole
}

export interface CreateProjectInput {
  code: string
  name: string
  allowedOrigins: string[]
  timezone: string
}

export interface CreatedProject {
  project: Project
  writeKey: string
}
export interface ProjectMember { user_id: number; project_id: number; username: string; display_name?: string; role: ProjectRole; is_active: boolean }
export interface WriteKeyStatus { previous_key_expires_at?: string }
export interface ProjectOverview {
  window: { start: string; end: string }
  invalid_count: number
  dead_letter_count: number
  queue: { pending: number; oldest_pending_age_seconds: number }
  no_report_points: Array<{ id: number; code: string; name: string; published_at: string }>
  no_report_threshold_hours: number
}

function mapProject(project: ApiProject): Project {
  return {
    id: project.id,
    code: project.code,
    name: project.name,
    allowedOrigins: project.allowed_origins ?? [],
    timezone: project.timezone,
    active: project.is_active,
    createdBy: project.created_by,
    role: project.role,
  }
}

export async function listProjects(): Promise<Project[]> {
  const { data } = await apiClient.get<{ projects: ApiProject[] }>('/projects')
  return data.projects.map(mapProject)
}

export async function createProject(input: CreateProjectInput): Promise<CreatedProject> {
  const { data } = await apiClient.post<{ project: ApiProject; write_key: string }>('/projects', {
    code: input.code,
    name: input.name,
    allowed_origins: input.allowedOrigins,
    timezone: input.timezone,
  })
  return { project: mapProject(data.project), writeKey: data.write_key }
}
export async function updateProjectSettings(projectId: number, input: Pick<CreateProjectInput, 'timezone'|'allowedOrigins'>) {
  const { data } = await apiClient.patch<ApiProject>(`/projects/${projectId}/settings`, { timezone: input.timezone, allowed_origins: input.allowedOrigins })
  return mapProject(data)
}
export async function listProjectMembers(projectId: number, signal?: AbortSignal) { return (await apiClient.get<{ members: ProjectMember[] }>(`/projects/${projectId}/members`, { signal })).data.members }
export async function addProjectMember(projectId: number, username: string, role: ProjectRole) { await apiClient.post(`/projects/${projectId}/members`, { username, role }) }
export async function removeProjectMember(projectId: number, userId: number) { await apiClient.delete(`/projects/${projectId}/members/${userId}`) }
export async function getWriteKeyStatus(projectId: number, signal?: AbortSignal) { return (await apiClient.get<WriteKeyStatus>(`/projects/${projectId}/write-key-status`, { signal })).data }
export async function rotateWriteKey(projectId: number) { return (await apiClient.post<{ write_key: string; previous_key_expires_at: string }>(`/projects/${projectId}/rotate-write-key`)).data }
export async function getProjectOverview(projectId: number, signal?: AbortSignal) { return (await apiClient.get<ProjectOverview>(`/projects/${projectId}/overview`, { signal })).data }
