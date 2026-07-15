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
