import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { describe, expect, it } from 'vitest'
import { createAppRouter } from '@/router'
import { useAuthStore } from '@/stores/auth'
import { useProjectStore } from '@/stores/project'
import AppLayout from './AppLayout.vue'

describe('management shell permissions', () => {
  it('shows read navigation but hides project settings from viewers', async () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    useAuthStore().setSession({
      accessToken: 'token',
      mustChangePassword: false,
      user: { id: 2, username: 'viewer', displayName: 'Viewer', mustChangePassword: false, status: 'ACTIVE' },
    })
    useProjectStore().$patch({
      projects: [{ id: 9, code: 'checkout', name: 'Checkout', allowedOrigins: [], timezone: 'Asia/Shanghai', active: true, createdBy: 1, role: 'VIEWER' }],
      currentProjectId: 9,
    })
    const router = createAppRouter()
    await router.push('/projects/9/overview')
    const wrapper = mount(AppLayout, { global: { plugins: [pinia, router], stubs: { RouterView: true } } })

    expect(wrapper.get('nav').text()).toContain('项目概览')
    expect(wrapper.get('nav').text()).toContain('点位管理')
    expect(wrapper.get('nav').text()).not.toContain('项目设置')
  })
})
