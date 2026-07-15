import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useProjectStore } from './project'
import * as projectsApi from '@/api/projects'

vi.mock('@/api/projects', () => ({ listProjects: vi.fn(), createProject: vi.fn() }))

describe('project context', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    setActivePinia(createPinia())
  })

  it('uses the explicit membership role and persists only the selected project id', async () => {
    vi.mocked(projectsApi.listProjects).mockResolvedValue([
      {
        id: 9,
        code: 'checkout',
        name: 'Checkout',
        allowedOrigins: [],
        timezone: 'Asia/Shanghai',
        active: true,
        createdBy: 1,
        role: 'VIEWER',
      },
    ])
    const projects = useProjectStore()

    await projects.load()

    expect(projects.currentRole).toBe('VIEWER')
    expect(projects.canMutate).toBe(false)
    expect(localStorage.getItem('go-dot:last-project-id')).toBe('9')
    expect(Object.keys(localStorage)).toEqual(['go-dot:last-project-id'])
  })

  it('creates the first project as an explicit admin membership and selects it', async () => {
    vi.mocked(projectsApi.listProjects).mockResolvedValue([])
    vi.mocked(projectsApi.createProject).mockResolvedValue({
      project: { id: 12, code: 'first', name: 'First', allowedOrigins: [], timezone: 'Asia/Shanghai', active: true, createdBy: 1, role: 'ADMIN' },
      writeKey: 'write-key-once',
    })
    const projects = useProjectStore()
    await projects.load()

    const created = await projects.create({ code: 'first', name: 'First', allowedOrigins: [], timezone: 'Asia/Shanghai' })

    expect(created.writeKey).toBe('write-key-once')
    expect(projects.currentRole).toBe('ADMIN')
    expect(projects.currentProjectId).toBe(12)
  })

  it('fails closed when project membership loading fails', async () => {
    vi.mocked(projectsApi.listProjects).mockRejectedValue(new Error('membership unavailable'))
    const projects = useProjectStore()

    await expect(projects.load()).rejects.toThrow('membership unavailable')

    expect(projects.loadState).toBe('error')
    expect(projects.canCreateInitialProject).toBe(false)
    await expect(projects.create({ code: 'unsafe', name: 'Unsafe', allowedOrigins: [], timezone: 'UTC' })).rejects.toThrow('onboarding unavailable')
    expect(projectsApi.createProject).not.toHaveBeenCalled()
  })
})
