import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { defineComponent, h, inject, provide, toRef, type InjectionKey, type Ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useProjectStore } from '@/stores/project'
import * as reportsApi from '@/api/reports'
import * as trackingApi from '@/api/tracking'
import CustomReportsView from './CustomReportsView.vue'

vi.mock('@/api/reports', () => ({
  listReports: vi.fn(),
  getReport: vi.fn(),
  createReport: vi.fn(),
  updateReport: vi.fn(),
  deleteReport: vi.fn(),
  queryDaily: vi.fn(),
  exportDaily: vi.fn(),
}))
vi.mock('@/api/tracking', () => ({ listPoints: vi.fn() }))
vi.mock('vue-router', () => ({ useRoute: () => ({ params: { projectId: '9' } }) }))

const rowsKey = Symbol('rows') as InjectionKey<Ref<Record<string, unknown>[]>>
const ElTable = defineComponent({
  name: 'ElTable',
  props: { data: { type: Array, default: () => [] } },
  setup(props, { slots }) {
    provide(rowsKey, toRef(props, 'data') as Ref<Record<string, unknown>[]>)
    return () => h('div', { 'data-test': 'result-table' }, slots.default?.())
  },
})
const ElTableColumn = defineComponent({
  name: 'ElTableColumn',
  props: { prop: String, label: String },
  setup(props, { slots }) {
    const rows = inject(rowsKey)
    return () => h('div', [
      h('strong', props.label ?? ''),
      ...(rows?.value ?? []).map((row) => h('span', slots.default?.({ row }) ?? String(row[props.prop ?? ''] ?? ''))),
    ])
  },
})
const ElSelect = defineComponent({
  name: 'ElSelect',
  props: ['modelValue'],
  emits: ['update:modelValue'],
  setup(_, { slots }) { return () => h('div', slots.default?.()) },
})
const ElOption = defineComponent({ name: 'ElOption', setup: () => () => h('span') })
const ElDatePicker = defineComponent({ name: 'ElDatePicker', props: ['modelValue'], emits: ['update:modelValue'], template: '<div />' })
const ElInput = defineComponent({
  name: 'ElInput',
  props: ['modelValue'],
  emits: ['update:modelValue'],
  template: '<input :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" />',
})
const ElDialog = defineComponent({
  name: 'ElDialog',
  props: ['modelValue'],
  emits: ['update:modelValue'],
  setup(props, { slots }) { return () => props.modelValue ? h('div', { role: 'dialog' }, [slots.default?.(), slots.footer?.()]) : null },
})

const stubs = {
  ElTable,
  ElTableColumn,
  ElSelect,
  ElOption,
  ElDatePicker,
  ElInput,
  ElDialog,
  ElForm: defineComponent({ setup: (_, { slots }) => () => h('form', slots.default?.()) }),
  ElFormItem: defineComponent({ props: ['label'], setup: (props, { slots }) => () => h('label', [props.label, slots.default?.()]) }),
  ElButton: defineComponent({ props: ['disabled'], emits: ['click'], setup: (props, { slots, emit, attrs }) => () => h('button', { ...attrs, disabled: props.disabled, onClick: () => emit('click') }, slots.default?.()) }),
  ElAlert: defineComponent({ props: ['title'], setup: (props) => () => h('div', { role: 'alert' }, props.title) }),
  ElCard: defineComponent({ setup: (_, { slots }) => () => h('div', slots.default?.()) }),
  ElTag: defineComponent({ setup: (_, { slots }) => () => h('span', slots.default?.()) }),
  ElEmpty: defineComponent({ props: ['description'], setup: (props) => () => h('div', props.description) }),
}

const point = (id: number, code: string, name: string, state: 'PUBLISHED' | 'DEPRECATED') => ({
  id, project_id: 9, category_id: 1, category: {}, category_path: [], code, name,
  behavior_type: 'CUSTOM', definition_state: state, development_state: 'ONLINE', count_pv: true,
  count_uv: true, uv_mode: id === 2 ? 'EXACT' : 'APPROXIMATE', time_basis: 'SERVER',
  supported_platforms: [], supported_channels: [], properties: [], created_by: 1, updated_by: 1,
}) as never

function setup(role: 'ADMIN' | 'VIEWER') {
  const pinia = createPinia()
  setActivePinia(pinia)
  const projects = useProjectStore()
  projects.projects = [{ id: 9, code: 'one', name: 'One', allowedOrigins: [], timezone: 'Asia/Shanghai', active: true, createdBy: 1, role }]
  projects.currentProjectId = 9
  return mount(CustomReportsView, { global: { plugins: [pinia], stubs, directives: { loading: () => undefined } } })
}

describe('CustomReportsView', () => {
  afterEach(() => vi.useRealTimers())

  beforeEach(() => {
    vi.clearAllMocks()
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-07-20T16:30:00Z')) // 2026-07-21 in Shanghai
    vi.mocked(reportsApi.listReports).mockResolvedValue([])
    vi.mocked(trackingApi.listPoints)
      .mockResolvedValueOnce({ items: [point(1, 'published', '已发布点位', 'PUBLISHED')], total: 1 })
      .mockResolvedValueOnce({ items: [point(2, 'deprecated', '已废弃点位', 'DEPRECATED')], total: 1 })
  })

  it('supports the complete admin query, ordered save, optimistic update, and edited export workflow', async () => {
    const result: reportsApi.DailyResult = {
      timezone: 'Asia/Shanghai', start_date: '2026-07-20', end_date: '2026-07-21', point_count: 2,
      day_count: 2, total_pv: 35,
      items: [
        { date: '2026-07-20', complete: true, point_id: 2, point_code: 'deprecated', point_name: '已废弃点位', pv: 12, uv: 8, uv_mode: 'EXACT' },
        { date: '2026-07-21', complete: false, point_id: 1, point_code: 'published', point_name: '已发布点位', pv: 23, uv: 15, uv_mode: 'APPROXIMATE' },
      ],
    }
    vi.mocked(reportsApi.queryDaily).mockResolvedValue(result)
    const saved = {
      id: 7, project_id: 9, name: '核心日报', description: '当前编辑', default_days: 30, points: [],
      created_by: 1, updated_by: 1, created_at: '2026-07-20T01:00:00Z', updated_at: 'snapshot-v1',
    }
    vi.mocked(reportsApi.createReport).mockResolvedValue(saved)
    vi.mocked(reportsApi.updateReport).mockResolvedValue({ ...saved, updated_at: 'snapshot-v2' })
    vi.mocked(reportsApi.exportDaily).mockResolvedValue({ blob: new Blob(['xlsx']), filename: '核心日报.xlsx' })
    const createObjectURL = vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:download')
    const revokeObjectURL = vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => undefined)
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => undefined)

    const wrapper = setup('ADMIN')
    await flushPromises()

    expect((wrapper.getComponent(ElDatePicker).props('modelValue') as string[])).toEqual(['2026-06-22', '2026-07-21'])
    expect(wrapper.text()).toContain('新建')
    expect(wrapper.text()).toContain('保存配置')
    expect(wrapper.text()).toContain('另存为')
    expect(wrapper.text()).toContain('删除')
    expect(trackingApi.listPoints).toHaveBeenNthCalledWith(1, 9, expect.objectContaining({ definition_state: 'PUBLISHED' }), expect.any(AbortSignal))
    expect(trackingApi.listPoints).toHaveBeenNthCalledWith(2, 9, expect.objectContaining({ definition_state: 'DEPRECATED' }), expect.any(AbortSignal))

    wrapper.findAllComponents(ElSelect)[1]!.vm.$emit('update:modelValue', [1, 2])
    await wrapper.vm.$nextTick()
    await wrapper.get('[data-test="move-point-down-1"]').trigger('click')
    wrapper.getComponent(ElDatePicker).vm.$emit('update:modelValue', ['2026-07-20', '2026-07-21'])
    await wrapper.get('[data-test="report-name"]').setValue('核心日报')
    await wrapper.get('[data-test="report-description"]').setValue('当前编辑')
    await wrapper.get('[data-test="query-report"]').trigger('click')
    await flushPromises()

    expect(reportsApi.queryDaily).toHaveBeenCalledWith({ project_id: 9, start_date: '2026-07-20', end_date: '2026-07-21', point_ids: [2, 1] }, expect.any(AbortSignal))
    expect(wrapper.text()).toContain('统计时区：Asia/Shanghai')
    expect(wrapper.text()).toContain('2026-07-20')
    expect(wrapper.text()).toContain('已完成')
    expect(wrapper.text()).toContain('未完整')
    expect(wrapper.text()).toContain('deprecated')
    expect(wrapper.text()).toContain('已废弃点位')
    expect(wrapper.text()).toContain('12')
    expect(wrapper.text()).toContain('8')
    expect(wrapper.text()).toContain('EXACT')
    expect(wrapper.text()).toContain('总 PV')
    expect(wrapper.text()).toContain('35')
    expect(wrapper.text()).not.toContain('总 UV')

    await wrapper.get('[data-test="save-as-report"]').trigger('click')
    await flushPromises()
    expect(reportsApi.createReport).toHaveBeenCalledWith(9, { name: '核心日报', description: '当前编辑', default_days: 30, point_ids: [2, 1] })

    await wrapper.get('[data-test="save-report"]').trigger('click')
    await flushPromises()
    expect(reportsApi.updateReport).toHaveBeenCalledWith(9, 7, {
      name: '核心日报', description: '当前编辑', default_days: 30, point_ids: [2, 1], expected_updated_at: 'snapshot-v1',
    })

    wrapper.getComponent(ElDatePicker).vm.$emit('update:modelValue', ['2026-07-19', '2026-07-20'])
    await wrapper.get('[data-test="export-report"]').trigger('click')
    await flushPromises()
    expect(reportsApi.exportDaily).toHaveBeenCalledWith({
      project_id: 9, start_date: '2026-07-19', end_date: '2026-07-20', point_ids: [2, 1],
      report_name: '核心日报', report_description: '当前编辑',
    })
    expect(createObjectURL).toHaveBeenCalledWith(expect.any(Blob))
    expect(click).toHaveBeenCalled()
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:download')
  })

  it('lets viewers load, query, and export while hiding every mutation control', async () => {
    const saved = {
      id: 8, project_id: 9, name: '只读日报', description: 'viewer', default_days: 7,
      points: [{ id: 2, code: 'deprecated', name: '已废弃点位', definition_state: 'DEPRECATED', uv_mode: 'EXACT', sort_no: 1 }],
      created_by: 1, updated_by: 1, created_at: '2026-07-20T01:00:00Z', updated_at: 'v1',
    } satisfies reportsApi.SavedReport
    vi.mocked(reportsApi.listReports).mockResolvedValue([saved])
    vi.mocked(reportsApi.getReport).mockResolvedValue(saved)
    vi.mocked(reportsApi.queryDaily).mockResolvedValue({
      timezone: 'Asia/Shanghai', start_date: '2026-07-21', end_date: '2026-07-21', point_count: 1, day_count: 1, total_pv: 2,
      items: [{ date: '2026-07-21', complete: false, point_id: 2, point_code: 'deprecated', point_name: '已废弃点位', pv: 2, uv: 1, uv_mode: 'EXACT' }],
    })
    vi.mocked(reportsApi.exportDaily).mockResolvedValue({ blob: new Blob(), filename: 'viewer.xlsx' })
    vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:viewer')
    vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => undefined)
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => undefined)

    const wrapper = setup('VIEWER')
    await flushPromises()

    const reportSelect = wrapper.findAllComponents(ElSelect)[0]!
    reportSelect.vm.$emit('update:modelValue', 8)
    await flushPromises()
    expect(reportsApi.getReport).toHaveBeenCalledWith(9, 8, expect.any(AbortSignal))
    expect((wrapper.getComponent(ElDatePicker).props('modelValue') as string[])).toEqual(['2026-07-15', '2026-07-21'])

    expect(wrapper.find('[data-test="report-select"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="query-report"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="export-report"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="new-report"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="save-report"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="save-as-report"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="delete-report"]').exists()).toBe(false)

    await wrapper.get('[data-test="query-report"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('2026-07-21')
    expect(wrapper.text()).toContain('未完整')
  })
})
