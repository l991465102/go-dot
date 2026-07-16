import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { describe, expect, it, vi } from 'vitest'
import { useProjectStore } from '@/stores/project'
import * as diagnosticsApi from '@/api/diagnostics'
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
