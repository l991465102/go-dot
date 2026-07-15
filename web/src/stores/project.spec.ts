import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useProjectStore } from './project'
import * as projectsApi from '@/api/projects'

vi.mock('@/api/projects', () => ({ listProjects: vi.fn() }))

describe('project context', () => {
  beforeEach(() => {
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
})
