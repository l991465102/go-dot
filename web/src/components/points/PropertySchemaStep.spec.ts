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

  it('only offers compatible analytics modes and metric aggregations', async () => {
    const model = draft()
    model.properties = [{
      code: 'amount', name: '金额', type: 'DECIMAL', required: false, sort_order: 0,
      analytics: { mode: 'METRIC', aggregations: ['COUNT'] },
    }]
    const wrapper = mount(PropertySchemaStep, { props: { modelValue: model } })
    const analyticsMode = wrapper.find('select[aria-label="统计方式"]')
    expect(analyticsMode.text()).toContain('数值指标')
    expect(analyticsMode.text()).not.toContain('分类维度')
    expect(wrapper.text()).toContain('有值次数')
    expect(wrapper.text()).toContain('平均值')

    const sum = wrapper.findAll('.aggregation-options input').find((input) => input.element.parentElement?.textContent?.includes('求和'))
    await sum!.setValue(true)
    const emitted = wrapper.emitted('update:modelValue')?.at(-1)?.[0] as PointDraft
    expect(emitted.properties[0]?.analytics?.aggregations).toEqual(['COUNT', 'SUM'])
  })
})
