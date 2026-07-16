import { render, screen, waitFor } from '@testing-library/vue'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ProjectOverviewView from './ProjectOverviewView.vue'
import { useProjectStore } from '@/stores/project'
import * as statisticsApi from '@/api/statistics'
import * as diagnosticsApi from '@/api/diagnostics'
import * as trackingApi from '@/api/tracking'
import * as projectsApi from '@/api/projects'

vi.mock('@/api/statistics', async (importOriginal) => ({ ...(await importOriginal<typeof import('@/api/statistics')>()), getRealtime: vi.fn(), getTrend: vi.fn() }))
vi.mock('@/api/diagnostics', () => ({ listInvalidEvents: vi.fn(), listDeadLetters: vi.fn() }))
vi.mock('@/api/tracking', () => ({ listPoints: vi.fn() }))
vi.mock('@/api/projects', async (importOriginal) => ({ ...(await importOriginal<typeof import('@/api/projects')>()), getProjectOverview: vi.fn() }))

describe('ProjectOverviewView', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    const project = useProjectStore()
    project.projects = [{ id: 9, code: 'mall', name: 'Mall', allowedOrigins: [], timezone: 'Asia/Shanghai', active: true, createdBy: 1, role: 'ADMIN' }]
    project.currentProjectId = 9
    vi.mocked(statisticsApi.getRealtime).mockResolvedValue({ pv: 12, uv: 7, approximate: true })
    vi.mocked(statisticsApi.getTrend).mockResolvedValue({ items: [{ period: '2026-07-15T02:00:00Z', pv: 8, uv: 5, approximate: true }] })
    vi.mocked(diagnosticsApi.listInvalidEvents).mockResolvedValue({ items: [{ error_code: 'PROPERTY_REQUIRED', error_message: 'missing', received_at: '2026-07-15T02:30:00Z' }], next_cursor: '' })
    vi.mocked(diagnosticsApi.listDeadLetters).mockResolvedValue({ items: [{ id: '1-0', deliveries: 5, first_seen: '2026-07-15T02:20:00Z' }], next_cursor: '' })
    vi.mocked(trackingApi.listPoints).mockResolvedValue({ items: [{ code: 'product_view' } as never], total: 1 })
    vi.mocked(projectsApi.getProjectOverview).mockResolvedValue({ window: { start: '2026-07-14T03:00:00Z', end: '2026-07-15T03:00:00Z' }, valid_count: 99, invalid_count: 1, invalid_rate: 0.01, dead_letter_count: 1, queue: { pending: 2, oldest_pending_age_seconds: 12 }, no_report_points: [], no_report_threshold_hours: 24 } as never)
  })

  it('separates realtime cards from the delayed ClickHouse trend', async () => {
    render(ProjectOverviewView, { global: { stubs: { TrendChart: { props: ['items'], template: '<div data-testid="trend-values">{{ items.map(item => item.pv).join(",") }}</div>' } } } })

    expect(await screen.findByText('实时 PV')).toBeTruthy()
    expect(await screen.findByText('实时 UV（近似）')).toBeTruthy()
    expect(screen.getByText('约 1 分钟延迟')).toBeTruthy()
    expect(screen.getByText('无效事件')).toBeTruthy()
    expect(screen.getByText('1.00%')).toBeTruthy()
    expect(screen.getByText(/1 无效 \/ 100 总事件/)).toBeTruthy()
    expect(screen.getByText('死信消息')).toBeTruthy()
    await waitFor(() => expect(screen.getByTestId('trend-values').textContent).toBe('8'))
    expect(screen.queryByText('20')).toBeNull()
  })
})
