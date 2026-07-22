import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import PointWorkbenchView from './PointWorkbenchView.vue'
import * as trackingApi from '@/api/tracking'

const mocks = vi.hoisted(() => ({ route: { params: { projectId: '9' }, query: {} as Record<string, string | undefined> }, replace: vi.fn(), push: vi.fn() }))
vi.mock('vue-router', () => ({ useRoute: () => mocks.route, useRouter: () => ({ replace: mocks.replace, push: mocks.push }) }))
vi.mock('@/stores/project', () => ({ useProjectStore: () => ({ canMutate: true }) }))
vi.mock('@/api/tracking', async () => {
  const actual = await vi.importActual<typeof import('@/api/tracking')>('@/api/tracking')
  return { ...actual, listCategories: vi.fn(), listPoints: vi.fn(), getPoint: vi.fn() }
})

const scene = { id: 3, project_id: 9, parent_id: 2, code: 'detail', name: '商品详情', level: 'SCENE' as const, sort_order: 0, active: true, created_by: 1 }
const draft = { id: 7, project_id: 9, category_id: 3, category: scene, category_path: [], code: 'product_detail_view', name: '商品详情浏览', behavior_type: 'PAGE_VIEW' as const, definition_state: 'DRAFT' as const, development_state: 'PENDING' as const, count_pv: true, count_uv: true, uv_mode: 'APPROXIMATE' as const, time_basis: 'CLIENT' as const, supported_platforms: ['web'], supported_channels: [], properties: [], created_by: 1, updated_by: 1 }

describe('PointWorkbenchView route draft editing', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.route.query = {}
    mocks.replace.mockImplementation(async (location: { query?: Record<string, string | undefined> }) => { mocks.route.query = location.query ?? {} })
    vi.mocked(trackingApi.listCategories).mockResolvedValue([scene])
    vi.mocked(trackingApi.listPoints).mockResolvedValue({ items: [], total: 0 })
  })

  it('opens only a validated current-project draft from the edit query', async () => {
    mocks.route.query = { edit: '7' }
    vi.mocked(trackingApi.getPoint).mockResolvedValue(draft)
    const wrapper = mount(PointWorkbenchView)
    await vi.waitFor(() => expect(wrapper.find('[aria-label="点位编辑向导"]').exists()).toBe(true))
    expect(trackingApi.getPoint).toHaveBeenCalledWith(9, 7, expect.any(AbortSignal))
    expect(mocks.replace).toHaveBeenCalled()
  })

  it('rejects a published point supplied through the edit query', async () => {
    mocks.route.query = { edit: '7' }
    vi.mocked(trackingApi.getPoint).mockResolvedValue({ ...draft, definition_state: 'PUBLISHED' })
    const wrapper = mount(PointWorkbenchView)
    await vi.waitFor(() => expect(wrapper.text()).toContain('只能编辑当前项目中的草稿点位'))
    expect(wrapper.find('[aria-label="点位编辑向导"]').exists()).toBe(false)
  })

  it('shows Chinese filter labels and submits the original enum values', async () => {
    const wrapper = mount(PointWorkbenchView)
    await vi.waitFor(() => expect(trackingApi.listPoints).toHaveBeenCalled())

    expect(wrapper.get('[aria-label="定义状态"]').text()).toContain('草稿')
    expect(wrapper.get('[aria-label="定义状态"]').text()).toContain('已发布')
    expect(wrapper.get('[aria-label="开发状态"]').text()).toContain('开发中')
    expect(wrapper.get('[aria-label="行为类型"]').text()).toContain('页面浏览')
    expect(wrapper.get('[aria-label="平台"]').text()).toContain('小程序')

    await wrapper.get('[aria-label="定义状态"]').setValue('PUBLISHED')
    await wrapper.get('[aria-label="开发状态"]').setValue('TESTING')
    await wrapper.get('[aria-label="行为类型"]').setValue('ELEMENT_CLICK')
    await wrapper.get('[aria-label="平台"]').setValue('mini_program')
    await wrapper.get('.filter-bar').trigger('submit')

    await vi.waitFor(() => expect(trackingApi.listPoints).toHaveBeenLastCalledWith(9, expect.objectContaining({
      definition_state: 'PUBLISHED',
      development_state: 'TESTING',
      behavior_type: 'ELEMENT_CLICK',
      platform: 'mini_program',
    }), expect.any(AbortSignal)))
  })
})
