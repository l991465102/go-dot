import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import TriggerStatisticsStep from './TriggerStatisticsStep.vue'
import type { PointDraft } from '@/api/tracking'

const draft = (): PointDraft => ({
  category_id: 0, code: '', name: '', description: '', behavior_type: '', development_state: 'PENDING',
  count_pv: true, count_uv: true, uv_mode: 'APPROXIMATE', time_basis: 'CLIENT', supported_platforms: ['web'],
  supported_channels: [], properties: [],
})

describe('TriggerStatisticsStep', () => {
  it('shows Chinese statistic labels and emits protocol values', async () => {
    const wrapper = mount(TriggerStatisticsStep, { props: { modelValue: draft() } })
    const [uvMode, timeBasis] = wrapper.findAll('select')

    expect(uvMode!.text()).toContain('近似统计（HLL）')
    expect(uvMode!.text()).toContain('精确统计')
    expect(timeBasis!.text()).toContain('客户端事件时间')
    expect(timeBasis!.text()).toContain('服务端接收时间')

    await uvMode!.setValue('EXACT')
    await timeBasis!.setValue('SERVER')
    expect(wrapper.emitted('update:modelValue')?.[0]?.[0]).toEqual(expect.objectContaining({ uv_mode: 'EXACT' }))
    expect(wrapper.emitted('update:modelValue')?.[1]?.[0]).toEqual(expect.objectContaining({ time_basis: 'SERVER' }))
  })
})
