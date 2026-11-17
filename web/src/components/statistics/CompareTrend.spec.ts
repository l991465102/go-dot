import { mount, flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import CompareTrend from './CompareTrend.vue'
const mocks=vi.hoisted(()=>({setOption:vi.fn(),listPoints:vi.fn(),getTrend:vi.fn()}))
vi.mock('echarts/core',()=>({init:vi.fn(()=>({setOption:mocks.setOption,resize:vi.fn(),dispose:vi.fn()})),use:vi.fn()}))
vi.mock('echarts/charts',()=>({LineChart:{}}))
vi.mock('echarts/components',()=>({GridComponent:{},LegendComponent:{},TooltipComponent:{}}))
vi.mock('echarts/renderers',()=>({CanvasRenderer:{}}))
vi.mock('@/api/tracking',()=>({listPoints:mocks.listPoints}))
vi.mock('@/api/statistics',()=>({getTrend:mocks.getTrend}))
describe('multi-point comparison',()=>{
 beforeEach(()=>{vi.stubGlobal('ResizeObserver',class{observe(){} disconnect(){}});mocks.setOption.mockReset();mocks.listPoints.mockResolvedValue({total:2,items:[{id:1,code:'view',name:'浏览',definition_state:'PUBLISHED',uv_mode:'EXACT'},{id:2,code:'order',name:'下单',definition_state:'PUBLISHED',uv_mode:'APPROXIMATE'}]});mocks.getTrend.mockImplementation(async(q:{pointCodes:string[]})=>({items:q.pointCodes[0]==='view'?[{period:'2026-01-01T00:00:00Z',pv:4,uv:2,approximate:false}]:[{period:'2026-01-01T01:00:00Z',pv:3,uv:1,approximate:true}]}))})
 it('queries points independently and leaves missing periods empty',async()=>{const wrapper=mount(CompareTrend,{global:{stubs:{SavedCharts:true}},props:{query:{projectId:1,start:'2026-01-01T00:00:00Z',end:'2026-01-02T00:00:00Z',timezone:'UTC',granularity:'hour'}}});await flushPromises();await wrapper.get('select[aria-label="对比点位"]').setValue(['view','order']);await wrapper.get('button').trigger('click');await flushPromises();expect(mocks.getTrend).toHaveBeenCalledTimes(2);expect(mocks.getTrend.mock.calls.map(c=>c[0].pointCodes)).toEqual([['view'],['order']]);expect(mocks.setOption).toHaveBeenCalled();const option=mocks.setOption.mock.calls.at(-1)?.[0];expect(option.series[0].data).toEqual([4,null]);expect(option.series[1].data).toEqual([null,3]);expect(wrapper.text()).toContain('—');wrapper.unmount()})
})
