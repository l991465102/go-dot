import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useProjectStore } from '@/stores/project'
import * as projectsApi from '@/api/projects'
import ProjectSettingsView from './ProjectSettingsView.vue'

vi.mock('@/api/projects', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/api/projects')>()),
  listProjectMembers: vi.fn(),
  getWriteKeyStatus: vi.fn(),
  updateProjectSettings: vi.fn(),
  addProjectMember: vi.fn(),
  removeProjectMember: vi.fn(),
  rotateWriteKey: vi.fn(),
}))

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => { resolve = done })
  return { promise, resolve }
}

function setup(role: 'ADMIN' | 'VIEWER' = 'ADMIN') {
  const pinia = createPinia()
  setActivePinia(pinia)
  const projects = useProjectStore()
  projects.projects = [
    { id: 9, code: 'one', name: 'One', allowedOrigins: ['https://one.test'], timezone: 'Asia/Shanghai', active: true, createdBy: 1, role },
    { id: 10, code: 'two', name: 'Two', allowedOrigins: ['https://two.test'], timezone: 'UTC', active: true, createdBy: 1, role },
  ]
  projects.currentProjectId = 9
  return { pinia, projects }
}

describe('ProjectSettingsView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(projectsApi.listProjectMembers).mockResolvedValue([])
    vi.mocked(projectsApi.getWriteKeyStatus).mockResolvedValue({ previous_key_active: false })
  })

  it('lets viewers inspect settings, members, and key status without rendering mutation controls', async () => {
    const { pinia } = setup('VIEWER')
    const wrapper = mount(ProjectSettingsView, { global: { plugins: [pinia] } })
    await flushPromises()

    expect(wrapper.text()).toContain('基础设置')
    expect(wrapper.text()).toContain('项目成员')
    expect(wrapper.text()).toContain('写入密钥')
    expect(wrapper.text()).not.toContain('保存设置')
    expect(wrapper.text()).not.toContain('添加或更新')
    expect(wrapper.text()).not.toContain('移除')
    expect(wrapper.text()).not.toContain('轮换写入密钥')
    expect(wrapper.find('[data-test="save-settings"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="member-mutation-form"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="remove-member"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="rotate-write-key"]').exists()).toBe(false)
  })

  it('clears a one-time key on project switch and ignores a late rotation response', async () => {
    const { pinia, projects } = setup()
    const rotation = deferred<{ write_key: string; previous_key_expires_at: string }>()
    vi.mocked(projectsApi.rotateWriteKey).mockReturnValue(rotation.promise)
    const wrapper = mount(ProjectSettingsView, { global: { plugins: [pinia] } })
    await flushPromises()

    await wrapper.get('[data-test="rotate-write-key"]').trigger('click')
    projects.currentProjectId = 10
    await flushPromises()
    projects.currentProjectId = 9
    await flushPromises()
    rotation.resolve({ write_key: 'secret-from-project-one', previous_key_expires_at: '2026-07-17T00:00:00Z' })
    await flushPromises()

    expect(wrapper.text()).not.toContain('secret-from-project-one')
  })

  it('keeps the current project members when an aborted load resolves late', async () => {
    const { pinia, projects } = setup()
    const first = deferred<projectsApi.ProjectMember[]>()
    vi.mocked(projectsApi.listProjectMembers)
      .mockReturnValueOnce(first.promise)
      .mockResolvedValueOnce([{ user_id: 2, project_id: 10, username: 'current-user', role: 'VIEWER', is_active: true }])
    const wrapper = mount(ProjectSettingsView, {
      global: {
        plugins: [pinia],
        stubs: { ElTable: { props: ['data'], template: '<div data-test="members">{{ data.map(item => item.username).join(",") }}</div>' } },
      },
    })

    projects.currentProjectId = 10
    await flushPromises()
    first.resolve([{ user_id: 1, project_id: 9, username: 'stale-user', role: 'ADMIN', is_active: true }])
    await flushPromises()

    expect(wrapper.text()).toContain('current-user')
    expect(wrapper.text()).not.toContain('stale-user')
  })

  it('renders the write-key expiry in Asia/Shanghai', async () => {
    const { pinia } = setup()
    vi.mocked(projectsApi.getWriteKeyStatus).mockResolvedValue({
      previous_key_active: true,
      previous_key_expires_at: '2026-07-20T02:51:03Z',
    })
    const wrapper = mount(ProjectSettingsView, {
      global: {
        plugins: [pinia],
        stubs: { ElAlert: { props: ['title'], template: '<div>{{ title }}</div>' } },
      },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('旧密钥仍可使用至 2026-07-20 10:51:03（Asia/Shanghai，轮换后最多 24 小时）')
    expect(wrapper.text()).not.toContain('2026-07-20T02:51:03Z')
  })
})
