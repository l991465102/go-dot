import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it } from 'vitest'
import { createAppRouter } from '@/router'
import { useAuthStore } from '@/stores/auth'

const adminUser = {
  id: 1,
  username: 'admin',
  displayName: 'Administrator',
  mustChangePassword: true,
  status: 'ACTIVE',
}

describe('authentication routing', () => {
  beforeEach(() => setActivePinia(createPinia()))

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
})
