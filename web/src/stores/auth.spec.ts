import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createAppRouter } from '@/router'
import { useAuthStore } from '@/stores/auth'
import * as authApi from '@/api/auth'
import * as projectsApi from '@/api/projects'

const adminUser = {
  id: 1,
  username: 'admin',
  displayName: 'Administrator',
  mustChangePassword: true,
  status: 'ACTIVE',
}

describe('authentication routing', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    vi.restoreAllMocks()
    setActivePinia(createPinia())
  })

  it('redirects a first-login session to change-password', async () => {
    const router = createAppRouter()
    const auth = useAuthStore()
    auth.setSession({ accessToken: 'token', mustChangePassword: true, user: adminUser })

    await router.push('/projects/1/overview')

    expect(router.currentRoute.value.name).toBe('change-password')
  })

  it('keeps access tokens out of browser storage', () => {
    const auth = useAuthStore()

    auth.setSession({ accessToken: 'memory-only', mustChangePassword: false, user: { ...adminUser, mustChangePassword: false } })

    expect(localStorage.length).toBe(0)
    expect(sessionStorage.length).toBe(0)
  })

  it('retains the local session when logout fails', async () => {
    vi.spyOn(authApi, 'logout').mockRejectedValue(new Error('service unavailable'))
    const auth = useAuthStore()
    auth.setSession({ accessToken: 'token', mustChangePassword: false, user: { ...adminUser, mustChangePassword: false } })

    await expect(auth.signOut()).rejects.toThrow('service unavailable')

    expect(auth.authenticated).toBe(true)
    expect(auth.user?.username).toBe('admin')
  })

  it('routes an authenticated account with no projects to onboarding', async () => {
    vi.spyOn(projectsApi, 'listProjects').mockResolvedValue([])
    const auth = useAuthStore()
    auth.setSession({ accessToken: 'token', mustChangePassword: false, user: { ...adminUser, mustChangePassword: false } })
    const router = createAppRouter()

    await router.push('/projects/9/overview')

    expect(router.currentRoute.value.name).toBe('project-onboarding')
  })

  it('restores a cookie session with no projects directly into onboarding', async () => {
    vi.spyOn(authApi, 'refresh').mockResolvedValue({ accessToken: 'restored', mustChangePassword: false, user: { ...adminUser, mustChangePassword: false } })
    vi.spyOn(projectsApi, 'listProjects').mockResolvedValue([])
    const router = createAppRouter()

    await router.push('/projects/9/overview')

    expect(router.currentRoute.value.name).toBe('project-onboarding')
  })

  it('does not expose onboarding to an existing viewer membership', async () => {
    vi.spyOn(projectsApi, 'listProjects').mockResolvedValue([
      { id: 9, code: 'visible', name: 'Visible', allowedOrigins: [], timezone: 'Asia/Shanghai', active: true, createdBy: 1, role: 'VIEWER' },
    ])
    const auth = useAuthStore()
    auth.setSession({ accessToken: 'token', mustChangePassword: false, user: { ...adminUser, id: 2, username: 'viewer', mustChangePassword: false } })
    const router = createAppRouter()

    await router.push('/onboarding')

    expect(router.currentRoute.value.name).toBe('project-overview')
    expect(router.currentRoute.value.params.projectId).toBe('9')
  })
})
