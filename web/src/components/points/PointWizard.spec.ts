import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import PointWizard from './PointWizard.vue'
import PointDetailView from '@/views/PointDetailView.vue'
import * as trackingApi from '@/api/tracking'

vi.mock('@/api/tracking', async () => {
  const actual = await vi.importActual<typeof import('@/api/tracking')>('@/api/tracking')
  return { ...actual, createPoint: vi.fn(), updateDraft: vi.fn(), publishPoint: vi.fn(), getPoint: vi.fn() }
})

const scene = { id: 3, project_id: 9, parent_id: 2, code: 'detail', name: '商品详情', level: 'SCENE' as const, sort_order: 0, active: true, created_by: 1 }
const moduleCategory = { ...scene, id: 2, level: 'MODULE' as const }
const baseDraft = {
  id: 7, project_id: 9, category_id: 3, category: scene, category_path: [], code: 'product_detail_view', name: '商品详情浏览',
  behavior_type: 'PAGE_VIEW' as const, definition_state: 'DRAFT' as const, development_state: 'PENDING' as const,
  count_pv: true, count_uv: true, uv_mode: 'APPROXIMATE' as const, time_basis: 'CLIENT' as const,
  supported_platforms: ['web'], supported_channels: [], properties: [], created_by: 1, updated_by: 1,
}

describe('PointWizard', () => {
  beforeEach(() => vi.clearAllMocks())

  it('rejects saving a point under a non-Scene category', async () => {
    const wrapper = mount(PointWizard, { props: { projectId: 9, categories: [moduleCategory], initialPoint: { ...baseDraft, category_id: 2, category: moduleCategory }, canMutate: true } })
    await wrapper.get('[data-test=save-draft]').trigger('click')
    expect(wrapper.text()).toContain('点位必须归属到业务场景')
    expect(trackingApi.updateDraft).not.toHaveBeenCalled()
  })

  it('prevents publishing an enum property without values', async () => {
    const wrapper = mount(PointWizard, { props: { projectId: 9, categories: [scene], initialPoint: { ...baseDraft, properties: [{ code: 'source', name: '来源', type: 'ENUM', required: false, enum_values: [], sort_order: 0 }] }, canMutate: true } })
    await wrapper.get('[data-test=publish]').trigger('click')
    expect(wrapper.text()).toContain('枚举属性至少需要一个枚举值')
    expect(trackingApi.publishPoint).not.toHaveBeenCalled()
  })

  it('saves an existing draft through PATCH then publishes it', async () => {
    vi.mocked(trackingApi.updateDraft).mockResolvedValue(baseDraft)
    vi.mocked(trackingApi.publishPoint).mockResolvedValue({ point_id: 7, point_code: baseDraft.code, version: 1 } as never)
    const wrapper = mount(PointWizard, { props: { projectId: 9, categories: [scene], initialPoint: baseDraft, canMutate: true } })
    await wrapper.get('[data-test=save-draft]').trigger('click')
    expect(trackingApi.updateDraft).toHaveBeenCalledWith(9, 7, expect.objectContaining({ code: baseDraft.code }))
    await wrapper.get('[data-test=publish]').trigger('click')
    expect(trackingApi.publishPoint).toHaveBeenCalledWith(9, 7)
  })

  it('does not publish stale edits until the current model is saved', async () => {
    vi.mocked(trackingApi.updateDraft).mockResolvedValue(baseDraft)
    const wrapper = mount(PointWizard, { props: { projectId: 9, categories: [scene], initialPoint: baseDraft, canMutate: true } })
    await wrapper.get('[data-test=save-draft]').trigger('click')
    await wrapper.get('input[placeholder="product_detail_view"]').setValue('product_detail_view_changed')
    await wrapper.get('[data-test=publish]').trigger('click')
    expect(wrapper.text()).toContain('请先保存当前修改')
    expect(trackingApi.publishPoint).not.toHaveBeenCalled()
  })

  it('renders a published point without edit actions for a viewer', async () => {
    vi.mocked(trackingApi.getPoint).mockResolvedValue({ ...baseDraft, definition_state: 'PUBLISHED' })
    const wrapper = mount(PointDetailView, { props: { projectId: 9, pointId: 7, canMutate: false } })
    await vi.waitFor(() => expect(wrapper.text()).toContain('商品详情浏览'))
    expect(wrapper.find('[data-test=edit-point]').exists()).toBe(false)
    expect(wrapper.find('[data-test=copy-point]').exists()).toBe(false)
  })
})
