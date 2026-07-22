import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import PropertySchemaStep from './PropertySchemaStep.vue'
import type { PointDraft } from '@/api/tracking'

const draft = (): PointDraft => ({
  category_id: 0, code: '', name: '', description: '', behavior_type: '', development_state: 'PENDING',
  count_pv: true, count_uv: true, uv_mode: 'APPROXIMATE', time_basis: 'CLIENT', supported_platforms: ['web'],
  supported_channels: [], properties: [{ code: 'tags', name: '标签', type: 'ARRAY', array_item_type: 'STRING', required: false, sort_order: 0 }],
})

describe('PropertySchemaStep', () => {
  it('shows Chinese property labels and emits protocol values', async () => {
    const wrapper = mount(PropertySchemaStep, { props: { modelValue: draft() } })
    const [propertyType, itemType] = wrapper.findAll('select')

    expect(propertyType!.text()).toContain('字符串')
    expect(propertyType!.text()).toContain('日期时间')
    expect(propertyType!.text()).toContain('数组')
    expect(itemType!.text()).toContain('整数')
    expect(itemType!.text()).not.toContain('枚举')

    await propertyType!.setValue('ENUM')
    await itemType!.setValue('BOOLEAN')
    expect((wrapper.emitted('update:modelValue')?.[0]?.[0] as PointDraft).properties[0]?.type).toBe('ENUM')
    expect((wrapper.emitted('update:modelValue')?.[1]?.[0] as PointDraft).properties[0]?.array_item_type).toBe('BOOLEAN')
  })
})
