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

const dialogStub = {
  props: ['modelValue'],
  template: '<div v-if="modelValue" data-test="rotation-confirm-dialog"><slot /><slot name="footer" /></div>',
}

describe('ProjectSettingsView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(projectsApi.listProjectMembers).mockResolvedValue([])
    vi.mocked(projectsApi.getWriteKeyStatus).mockResolvedValue({ previous_key_active: false })
  })

  it('lets viewers inspect settings, members, and key status without rendering mutation controls', async () => {
    const { pinia } = setup('VIEWER')
    const wrapper = mount(ProjectSettingsView, {
      global: { plugins: [pinia], stubs: { ElDialog: dialogStub } },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('基础设置')
    expect(wrapper.text()).toContain('项目成员')
    expect(wrapper.text()).toContain('写入密钥')
    expect(wrapper.text()).toContain('当前写入密钥永久有效，不会自动轮换。仅在确认密钥泄露时执行紧急轮换。')
    expect(wrapper.text()).not.toContain('保存设置')
    expect(wrapper.text()).not.toContain('添加或更新')
    expect(wrapper.text()).not.toContain('移除')
    expect(wrapper.text()).not.toContain('轮换写入密钥')
    expect(wrapper.find('[data-test="save-settings"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="member-mutation-form"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="remove-member"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="rotate-write-key"]').exists()).toBe(false)
  })

  it('requires an admin to explicitly confirm an emergency write-key rotation', async () => {
    const { pinia } = setup()
    vi.mocked(projectsApi.rotateWriteKey).mockResolvedValue({
      write_key: 'one-time-secret',
      previous_key_expires_at: '2026-07-20T02:51:03Z',
    })
    const wrapper = mount(ProjectSettingsView, {
      global: { plugins: [pinia], stubs: { ElDialog: dialogStub } },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('当前写入密钥永久有效，不会自动轮换。仅在确认密钥泄露时执行紧急轮换。')
    expect(wrapper.get('[data-test="rotate-write-key"]').text()).toBe('紧急轮换写入密钥')

    await wrapper.get('[data-test="rotate-write-key"]').trigger('click')

    expect(wrapper.get('[data-test="rotation-confirm-dialog"]').text()).toContain(
      '轮换后所有上报端都需要切换到新密钥。旧密钥仅保留 24 小时，请确认已准备好协调各端更新。',
    )
    expect(projectsApi.rotateWriteKey).not.toHaveBeenCalled()

    await wrapper.get('[data-test="cancel-key-rotation"]').trigger('click')

    expect(wrapper.find('[data-test="rotation-confirm-dialog"]').exists()).toBe(false)
    expect(projectsApi.rotateWriteKey).not.toHaveBeenCalled()

    await wrapper.get('[data-test="rotate-write-key"]').trigger('click')
    await wrapper.get('[data-test="confirm-key-rotation"]').trigger('click')
    await flushPromises()

    expect(projectsApi.rotateWriteKey).toHaveBeenCalledTimes(1)
    expect(projectsApi.rotateWriteKey).toHaveBeenCalledWith(9)
  })

  it('shows the one-time key and Shanghai expiry after a confirmed rotation', async () => {
    const { pinia } = setup()
    vi.mocked(projectsApi.rotateWriteKey).mockResolvedValue({
      write_key: 'one-time-secret',
      previous_key_expires_at: '2026-07-20T02:51:03Z',
    })
    const wrapper = mount(ProjectSettingsView, {
      global: {
        plugins: [pinia],
        stubs: {
          ElDialog: dialogStub,
          ElAlert: { props: ['title'], template: '<div>{{ title }}</div>' },
        },
      },
    })
    await flushPromises()

    await wrapper.get('[data-test="rotate-write-key"]').trigger('click')
    await wrapper.get('[data-test="confirm-key-rotation"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('one-time-secret')
    expect(wrapper.text()).toContain('旧密钥仍可使用至 2026-07-20 10:51:03（Asia/Shanghai，轮换后最多 24 小时）')
  })

  it('shows the existing error state without a plaintext key when rotation fails', async () => {
    const { pinia } = setup()
    vi.mocked(projectsApi.rotateWriteKey).mockRejectedValue(new Error('密钥仍在宽限期'))
    const wrapper = mount(ProjectSettingsView, {
      global: { plugins: [pinia], stubs: { ElDialog: dialogStub } },
    })
    await flushPromises()

    await wrapper.get('[data-test="rotate-write-key"]').trigger('click')
    await wrapper.get('[data-test="confirm-key-rotation"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[role="alert"]').text()).toContain('密钥仍在宽限期')
    expect(wrapper.find('.write-key-box').exists()).toBe(false)
  })

  it('clears a one-time key on project switch and ignores a late rotation response', async () => {
    const { pinia, projects } = setup()
    const rotation = deferred<{ write_key: string; previous_key_expires_at: string }>()
    vi.mocked(projectsApi.rotateWriteKey).mockReturnValue(rotation.promise)
    const wrapper = mount(ProjectSettingsView, {
      global: { plugins: [pinia], stubs: { ElDialog: dialogStub } },
    })
    await flushPromises()

    await wrapper.get('[data-test="rotate-write-key"]').trigger('click')
    await wrapper.get('[data-test="confirm-key-rotation"]').trigger('click')
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

  it('shows an active previous key with a missing expiry as a dash', async () => {
    const { pinia } = setup()
    vi.mocked(projectsApi.getWriteKeyStatus).mockResolvedValue({ previous_key_active: true })
    const wrapper = mount(ProjectSettingsView, {
      global: {
        plugins: [pinia],
        stubs: { ElAlert: { props: ['title'], template: '<div>{{ title }}</div>' } },
      },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('旧密钥仍可使用至 -（Asia/Shanghai，轮换后最多 24 小时）')
    expect(wrapper.text()).not.toContain('当前没有处于宽限期的旧密钥。')
  })
})
