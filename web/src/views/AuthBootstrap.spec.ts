import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import * as authApi from '@/api/auth'
import * as projectsApi from '@/api/projects'
import { createAppRouter } from '@/router'
import { useAuthStore } from '@/stores/auth'
import ChangePasswordView from './ChangePasswordView.vue'
import LoginView from './LoginView.vue'

const user = { id: 1, username: 'admin', displayName: 'admin', mustChangePassword: false, status: 'ACTIVE' }

describe('auth success followed by project bootstrap failure', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.restoreAllMocks()
  })

  it('keeps a successful login and does not report a credential error', async () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    useAuthStore().clearSession()
    vi.spyOn(authApi, 'login').mockResolvedValue({ accessToken: 'token', mustChangePassword: false, user })
    vi.spyOn(projectsApi, 'listProjects').mockRejectedValue(new Error('project service unavailable'))
    const router = createAppRouter()
    await router.push('/login')
    const wrapper = mount(LoginView, { attachTo: document.body, global: { plugins: [pinia, router, ElementPlus] } })
    const loginInputs = wrapper.findAll('input')
    await loginInputs[0]!.setValue('admin')
    await loginInputs[1]!.setValue('admin')

    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(useAuthStore().authenticated).toBe(true)
    expect(router.currentRoute.value.name).toBe('project-bootstrap-error')
    expect(wrapper.text()).not.toContain('账号或密码错误')
    wrapper.unmount()
  })

  it('keeps a successful password mutation and does not ask for the old password again', async () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const auth = useAuthStore()
    auth.setSession({ accessToken: 'token', mustChangePassword: true, user: { ...user, mustChangePassword: true } })
    vi.spyOn(authApi, 'changePassword').mockResolvedValue(undefined)
    vi.spyOn(projectsApi, 'listProjects').mockRejectedValue(new Error('project service unavailable'))
    const router = createAppRouter()
    await router.push('/change-password')
    const wrapper = mount(ChangePasswordView, { attachTo: document.body, global: { plugins: [pinia, router, ElementPlus] } })
    const passwordInputs = wrapper.findAll('input')
    await passwordInputs[0]!.setValue('admin')
    await passwordInputs[1]!.setValue('Admin-new-2026')
    await passwordInputs[2]!.setValue('Admin-new-2026')

    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(auth.mustChangePassword).toBe(false)
    expect(router.currentRoute.value.name).toBe('project-bootstrap-error')
    expect(wrapper.text()).not.toContain('密码修改失败')
    wrapper.unmount()
  })
})
