import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import TrendChart from './TrendChart.vue'

const setOption = vi.fn()
vi.mock('echarts/core', () => ({
  init: vi.fn(() => ({ setOption, resize: vi.fn(), dispose: vi.fn() })),
  use: vi.fn(),
}))
vi.mock('echarts/charts', () => ({ LineChart: {} }))
vi.mock('echarts/components', () => ({ GridComponent: {}, LegendComponent: {}, TooltipComponent: {} }))
vi.mock('echarts/renderers', () => ({ CanvasRenderer: {} }))

describe('TrendChart', () => {
  beforeEach(() => {
    setOption.mockClear()
    vi.stubGlobal('ResizeObserver', class { observe() {}; disconnect() {} })
  })

  it('formats periods in the selected project timezone', async () => {
    mount(TrendChart, {
      props: {
        items: [{ period: '2026-07-15T00:00:00Z', pv: 1, uv: 1, approximate: false }],
        timezone: 'Asia/Shanghai',
      } as never,
    })
    await vi.waitFor(() => expect(setOption).toHaveBeenCalled())
    const option = setOption.mock.calls.at(-1)?.[0]
    const expected = new Intl.DateTimeFormat('zh-CN', {
      timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hourCycle: 'h23',
    }).format(new Date('2026-07-15T00:00:00Z'))
    expect(option.xAxis.data).toEqual([expected])
  })

  it('redraws labels when the selected project timezone changes', async () => {
    const wrapper = mount(TrendChart, {
      props: {
        items: [{ period: '2026-07-15T00:00:00Z', pv: 1, uv: 1, approximate: false }],
        timezone: 'Asia/Shanghai',
      },
    })
    await vi.waitFor(() => expect(setOption).toHaveBeenCalled())
    setOption.mockClear()

    await wrapper.setProps({ timezone: 'America/Los_Angeles' })

    expect(setOption).toHaveBeenCalledOnce()
    const option = setOption.mock.calls[0]?.[0]
    const expected = new Intl.DateTimeFormat('zh-CN', {
      timeZone: 'America/Los_Angeles', year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hourCycle: 'h23',
    }).format(new Date('2026-07-15T00:00:00Z'))
    expect(option.xAxis.data).toEqual([expected])
  })
})
