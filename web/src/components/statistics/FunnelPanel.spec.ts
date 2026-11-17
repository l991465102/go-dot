import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import FunnelPanel from './FunnelPanel.vue'
const mocks=vi.hoisted(()=>({listPoints:vi.fn(),post:vi.fn()}))
vi.mock('@/api/tracking',()=>({listPoints:mocks.listPoints}))
vi.mock('@/api/client',()=>({apiClient:{post:mocks.post}}))
describe('funnel analysis',()=>{
 it('submits ordered point codes and displays conversion with an incomplete warning',async()=>{
  mocks.listPoints.mockResolvedValue({total:2,items:[{id:1,code:'page_view',name:'浏览',definition_state:'PUBLISHED'},{id:2,code:'order_submit',name:'下单',definition_state:'PUBLISHED'}]})
  mocks.post.mockResolvedValue({data:{incomplete:true,evaluated_until:'2026-01-01T01:00:00Z',steps:[{point_code:'page_view',visitors:10,conversion:null},{point_code:'order_submit',visitors:5,conversion:0.5}]}})
  const wrapper=mount(FunnelPanel,{global:{stubs:{SavedCharts:true}},props:{projectId:1}});await flushPromises()
  const selects=wrapper.findAll('select');await selects[0]!.setValue('page_view');await selects[1]!.setValue('order_submit');await wrapper.get('form').trigger('submit');await flushPromises()
  expect(mocks.post).toHaveBeenCalledWith('/statistics/funnel',expect.objectContaining({project_id:1,point_codes:['page_view','order_submit']}));expect(wrapper.text()).toContain('50.00%');expect(wrapper.text()).toContain('尚未结束')
 })
})
