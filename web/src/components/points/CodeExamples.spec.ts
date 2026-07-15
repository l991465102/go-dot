import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import CodeExamples from './CodeExamples.vue'

describe('CodeExamples', () => {
  it('includes an event id in JavaScript, Go and cURL examples', async () => {
    const wrapper = mount(CodeExamples, { props: { projectId: 9, pointCode: 'product_detail_view', platform: 'web', properties: [] } })
    expect(wrapper.text()).toContain('event_id')
    await wrapper.get('.code-tabs button:nth-child(2)').trigger('click')
    expect(wrapper.text()).toContain('event_id')
    await wrapper.get('.code-tabs button:nth-child(3)').trigger('click')
    expect(wrapper.text()).toContain('event_id')
  })

  it('uses an RFC3339 fallback for DATETIME properties', () => {
    const wrapper = mount(CodeExamples, { props: { projectId: 9, pointCode: 'product_detail_view', platform: 'web', properties: [{ code: 'occurred_at', name: '发生时间', type: 'DATETIME', required: true, sort_order: 0 }] } })
    expect(wrapper.text()).toContain('2026-07-15T08:00:00Z')
  })
})
