import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ProjectOnboardingView from './ProjectOnboardingView.vue'

vi.mock('vue-router', () => ({ useRouter: () => ({ replace: vi.fn() }) }))

const ElSelect = { props: ['modelValue'], template: '<select><slot /></select>' }
const ElOption = { props: ['label', 'value'], template: '<option :value="value">{{ label }}</option>' }

describe('ProjectOnboardingView', () => {
  beforeEach(() => setActivePinia(createPinia()))

  it('shows Chinese timezone labels with protocol values', () => {
    const wrapper = mount(ProjectOnboardingView, {
      global: { stubs: { ElSelect, ElOption, ElInput: true, ElButton: true } },
    })
    const options = wrapper.get('select').findAll('option')
    expect(options.map((option) => [option.attributes('value'), option.text()])).toEqual([
      ['Asia/Shanghai', '上海时区（UTC+8）'],
      ['UTC', '协调世界时（UTC）'],
    ])
  })
})
