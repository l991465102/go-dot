import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { defineComponent, h, inject, provide, toRef, type InjectionKey, type Ref } from 'vue'
import { describe, expect, it, vi } from 'vitest'
import { useProjectStore } from '@/stores/project'
import * as diagnosticsApi from '@/api/diagnostics'
import DeadLetterTable from '@/components/diagnostics/DeadLetterTable.vue'
import InvalidEventTable from '@/components/diagnostics/InvalidEventTable.vue'
import DiagnosticsView from './DiagnosticsView.vue'

vi.mock('@/api/diagnostics', () => ({
  listInvalidEvents: vi.fn(),
  listRecentEvents: vi.fn(),
  listDeadLetters: vi.fn(),
}))

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => { resolve = done })
  return { promise, resolve }
}

const tableRows = Symbol('tableRows') as InjectionKey<Ref<Record<string, unknown>[]>>
const tableStubs = {
  ElTable: defineComponent({
    props: { data: { type: Array, default: () => [] } },
    setup(props, { slots }) {
      provide(tableRows, toRef(props, 'data') as Ref<Record<string, unknown>[]>)
      return () => h('div', slots.default?.())
    },
  }),
  ElTableColumn: defineComponent({
    props: { prop: String, label: String },
    setup(props, { slots }) {
      const rows = inject(tableRows)
      return () => h('div', [
        h('span', props.label ?? ''),
        ...(rows?.value ?? []).map((row) => h('div', slots.default?.({ row }) ?? String(row[props.prop ?? ''] ?? ''))),
      ])
    },
  }),
}

describe('DiagnosticsView request ownership', () => {
  it('does not let a stale project request replace the current results', async () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const projects = useProjectStore()
    projects.projects = [
      { id: 9, code: 'one', name: 'One', allowedOrigins: [], timezone: 'UTC', active: true, createdBy: 1, role: 'ADMIN' },
      { id: 10, code: 'two', name: 'Two', allowedOrigins: [], timezone: 'UTC', active: true, createdBy: 1, role: 'ADMIN' },
    ]
    projects.currentProjectId = 9
    const first = deferred<diagnosticsApi.CursorPage<diagnosticsApi.InvalidEvent>>()
    vi.mocked(diagnosticsApi.listInvalidEvents)
      .mockReturnValueOnce(first.promise)
      .mockResolvedValueOnce({ items: [{ error_code: 'CURRENT', error_message: 'current-project', received_at: '2026-07-16T00:00:00Z' }] })
    const wrapper = mount(DiagnosticsView, {
      global: {
        plugins: [pinia],
        stubs: { InvalidEventTable: { props: ['items'], template: '<div>{{ items.map(item => item.error_code).join(",") }}</div>' } },
      },
    })

    projects.currentProjectId = 10
    await flushPromises()
    first.resolve({ items: [{ error_code: 'STALE', error_message: 'stale-project', received_at: '2026-07-15T00:00:00Z' }] })
    await flushPromises()

    expect(wrapper.text()).toContain('CURRENT')
    expect(wrapper.text()).not.toContain('STALE')
  })
})

describe('DiagnosticsView absolute timestamps', () => {
  it('renders invalid and dead-letter timestamps in Asia/Shanghai', () => {
    const invalid = mount(InvalidEventTable, {
      props: { items: [{ error_code: 'INVALID', error_message: 'invalid', received_at: '2026-07-20T02:51:03Z' }] },
      global: { stubs: tableStubs },
    })
    const dead = mount(DeadLetterTable, {
      props: { items: [{ id: '1-0', deliveries: 1, first_seen: '2026-07-20T02:51:03Z' }] },
      global: { stubs: tableStubs },
    })

    expect(invalid.text()).toContain('接收时间（Asia/Shanghai）')
    expect(invalid.text()).toContain('2026-07-20 10:51:03')
    expect(invalid.text()).not.toContain('2026-07-20T02:51:03Z')
    expect(dead.text()).toContain('首次失败（Asia/Shanghai）')
    expect(dead.text()).toContain('2026-07-20 10:51:03')
    expect(dead.text()).not.toContain('2026-07-20T02:51:03Z')
  })

  it('renders recent-event timestamps in Asia/Shanghai', async () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const projects = useProjectStore()
    projects.projects = [
      { id: 9, code: 'one', name: 'One', allowedOrigins: [], timezone: 'UTC', active: true, createdBy: 1, role: 'ADMIN' },
    ]
    projects.currentProjectId = 9
    vi.mocked(diagnosticsApi.listInvalidEvents).mockResolvedValue({ items: [] })
    vi.mocked(diagnosticsApi.listRecentEvents).mockResolvedValue({
      items: [{ event_id: 'event-1', point_code: 'launch', platform: 'ios', received_at: '2026-07-20T02:51:03Z', client_event_at: '2026-07-20T02:51:03Z', statistic_at: '2026-07-20T02:51:03Z' }],
    })
    const ElTabs = defineComponent({
      emits: ['update:modelValue', 'tabChange'],
      setup(_, { emit, slots }) {
        return () => h('div', [
          h('button', { 'data-test': 'recent-tab', onClick: () => { emit('update:modelValue', 'recent'); emit('tabChange', 'recent') } }, '最近事件'),
          slots.default?.(),
        ])
      },
    })
    const wrapper = mount(DiagnosticsView, {
      global: { plugins: [pinia], stubs: { ...tableStubs, ElTabs, ElTabPane: true } },
    })
    await flushPromises()

    await wrapper.get('[data-test="recent-tab"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('接收时间（Asia/Shanghai）')
    expect(wrapper.text()).toContain('2026-07-20 10:51:03')
    expect(wrapper.text()).not.toContain('2026-07-20T02:51:03Z')
  })
})
