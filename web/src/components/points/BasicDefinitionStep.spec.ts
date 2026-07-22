import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import BasicDefinitionStep from './BasicDefinitionStep.vue'
import type { PointDraft } from '@/api/tracking'

const draft = (): PointDraft => ({
  category_id: 0, code: '', name: '', description: '', behavior_type: '', development_state: 'PENDING',
  count_pv: true, count_uv: true, uv_mode: 'APPROXIMATE', time_basis: 'CLIENT', supported_platforms: ['web'],
  supported_channels: [], properties: [],
})

describe('BasicDefinitionStep', () => {
  it('shows Chinese behavior labels and emits the protocol value', async () => {
    const wrapper = mount(BasicDefinitionStep, { props: { modelValue: draft(), categories: [] } })
    const behavior = wrapper.findAll('select')[1]!

    expect(behavior.text()).toContain('页面浏览')
    expect(behavior.text()).toContain('元素曝光')
    expect(behavior.text()).toContain('业务结果')

    await behavior.setValue('ELEMENT_CLICK')
    expect(wrapper.emitted('update:modelValue')?.[0]?.[0]).toEqual(expect.objectContaining({ behavior_type: 'ELEMENT_CLICK' }))
  })
})
