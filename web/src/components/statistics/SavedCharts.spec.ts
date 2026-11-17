import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import SavedCharts from './SavedCharts.vue'
const api=vi.hoisted(()=>({listCharts:vi.fn(),createChart:vi.fn(),deleteChart:vi.fn()}))
vi.mock('@/stores/project',()=>({useProjectStore:()=>({canMutate:true})}))
vi.mock('@/api/charts',()=>api)
describe('saved charts',()=>{
 it('saves and reloads an ordered configuration',async()=>{
  const config={point_codes:['page_view','order_submit'],start:'2026-01-01T00:00:00Z',end:'2026-01-02T00:00:00Z',window_hours:24}
  api.listCharts.mockResolvedValue([{id:8,name:'下单漏斗',config}]);api.createChart.mockResolvedValue({id:8})
  const wrapper=mount(SavedCharts,{props:{projectId:1,kind:'FUNNEL',config}});await flushPromises();await wrapper.get('input').setValue('下单漏斗');await wrapper.get('button:nth-of-type(2)').trigger('click');await flushPromises();expect(api.createChart).toHaveBeenCalledWith(1,'FUNNEL','下单漏斗',config);await wrapper.get('button').trigger('click');expect(wrapper.emitted('load')?.[0]?.[0]).toEqual(config)
 })
})
