import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import CollaborationPublishStep from './CollaborationPublishStep.vue'

describe('CollaborationPublishStep', () => {
  it('renders the resolved full category path and property contract', () => {
    const categories = [
      { id: 1, project_id: 9, code: 'mall', name: '商城', level: 'DOMAIN' as const, sort_order: 0, active: true, created_by: 1 },
      { id: 2, project_id: 9, parent_id: 1, code: 'product', name: '商品', level: 'MODULE' as const, sort_order: 0, active: true, created_by: 1 },
      { id: 3, project_id: 9, parent_id: 2, code: 'detail', name: '商品详情', level: 'SCENE' as const, sort_order: 0, active: true, created_by: 1 },
    ]
    const modelValue = { category_id: 3, code: 'product_detail_view', name: '商品详情浏览', behavior_type: 'PAGE_VIEW' as const, development_state: 'PENDING' as const, trigger_timing: '加载完成', count_pv: true, count_uv: true, uv_mode: 'APPROXIMATE' as const, time_basis: 'CLIENT' as const, supported_platforms: ['web'], supported_channels: [], product_owner: '产品甲', developer_owner: '开发乙', properties: [
      { code: 'source', name: '来源', type: 'ENUM' as const, required: true, enum_values: ['home', 'search'], sort_order: 0 },
      { code: 'tags', name: '标签', type: 'ARRAY' as const, required: false, array_item_type: 'STRING' as const, sort_order: 1 },
    ] }
    const wrapper = mount(CollaborationPublishStep, { props: { modelValue, categories, projectId: 9 } })
    expect(wrapper.text()).toContain('商城 / 商品 / 商品详情')
    expect(wrapper.text()).toContain('source')
    expect(wrapper.text()).toContain('来源')
    expect(wrapper.text()).toContain('ENUM')
    expect(wrapper.text()).toContain('必填')
    expect(wrapper.text()).toContain('home、search')
    expect(wrapper.text()).toContain('tags')
    expect(wrapper.text()).toContain('ARRAY<STRING>')
  })
})
