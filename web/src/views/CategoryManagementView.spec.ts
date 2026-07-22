import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import CategoryManagementView from './CategoryManagementView.vue'
import * as trackingApi from '@/api/tracking'

vi.mock('vue-router', () => ({ useRoute: () => ({ params: { projectId: '9' } }) }))
vi.mock('@/stores/project', () => ({ useProjectStore: () => ({ canMutate: true }) }))
vi.mock('@/api/tracking', async () => {
  const actual = await vi.importActual<typeof import('@/api/tracking')>('@/api/tracking')
  return { ...actual, listCategories: vi.fn(), createCategory: vi.fn(), updateCategory: vi.fn() }
})

describe('CategoryManagementView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(trackingApi.listCategories).mockResolvedValue([])
  })

  it('shows Chinese category levels with their protocol values', async () => {
    const wrapper = mount(CategoryManagementView)
    await vi.waitFor(() => expect(trackingApi.listCategories).toHaveBeenCalled())
    await wrapper.get('.page-heading button').trigger('click')

    const options = wrapper.get('select').findAll('option')
    expect(options.map((option) => [option.attributes('value'), option.text()])).toEqual([
      ['DOMAIN', '业务域'], ['MODULE', '业务模块'], ['SCENE', '业务场景'],
    ])
  })
})
