<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useProjectStore } from '@/stores/project'
import { completedHourRange, getRealtime, getTrend, type StatisticsSummary, type TrendPoint } from '@/api/statistics'
import { listPoints } from '@/api/tracking'
import { getProjectOverview, type ProjectOverview } from '@/api/projects'
import MetricCards from '@/components/statistics/MetricCards.vue'
import TrendChart from '@/components/statistics/TrendChart.vue'

const projects = useProjectStore()
const project = computed(() => projects.currentProject)
const realtime = ref<StatisticsSummary>()
const trend = ref<TrendPoint[]>([])
const invalidCount = ref(0)
const deadLetterCount = ref(0)
const loading = ref(false)
const error = ref('')
const selectedPoint = ref('')
const operations = ref<ProjectOverview>()
let controller: AbortController | undefined

async function load() {
  const current = project.value
  if (!current) return
  controller?.abort()
  controller = new AbortController()
  const signal = controller.signal
  loading.value = true; error.value = ''; realtime.value = undefined; trend.value = []
  try {
    const points = await listPoints(current.id, { definition_state: 'PUBLISHED', page: 1, page_size: 1 }, signal)
    selectedPoint.value = points.items[0]?.code ?? ''
    const range = completedHourRange(24, current.timezone)
    const base = { projectId: current.id, ...range, timezone: current.timezone, granularity: 'hour' as const }
    const [history, overview] = await Promise.all([
      getTrend(base, signal),
      getProjectOverview(current.id, signal),
    ])
    if (signal.aborted) return
    trend.value = history.items
    operations.value = overview
    invalidCount.value = overview.invalid_count
    deadLetterCount.value = overview.dead_letter_count
    if (selectedPoint.value) realtime.value = await getRealtime({ ...base, pointCodes: [selectedPoint.value], start: new Date(new Date(range.end).getTime() - 3_600_000).toISOString() }, signal)
  } catch (cause) {
    if (!signal.aborted) error.value = cause instanceof Error ? cause.message : '概览加载失败'
  } finally { if (!signal.aborted) loading.value = false }
}
watch(() => projects.currentProjectId, load, { immediate: true })
onBeforeUnmount(() => controller?.abort())
</script>

<template>
  <section aria-labelledby="overview-title">
    <header class="page-heading"><div><p class="eyebrow">PROJECT OVERVIEW</p><h1 id="overview-title">项目概览</h1><p>快速了解 {{ project?.name }} 的业务数据与质量状态。</p></div><el-tag v-if="project" type="success" effect="light">{{ project.active ? '运行中' : '已停用' }}</el-tag></header>
    <div v-if="error" class="page-error" role="alert">{{ error }} <el-button @click="load">重试</el-button></div>
    <MetricCards :realtime="realtime" :realtime-point="selectedPoint" :invalid-count="invalidCount" :dead-letter-count="deadLetterCount" :loading="loading" />
    <article class="overview-card trend-panel"><header><div><p class="eyebrow">CLICKHOUSE TREND</p><h2>项目近 24 小时趋势</h2></div><div><el-tag effect="plain">约 1 分钟延迟</el-tag></div></header><TrendChart v-if="trend.length" :items="trend" /><el-empty v-else-if="!loading" description="暂无已聚合数据" /></article>
    <div class="operations-grid">
      <article class="overview-card"><p>队列待处理消息</p><strong>{{ operations?.queue.pending ?? '—' }}</strong><small>最老待处理 {{ operations ? operations.queue.oldest_pending_age_seconds + ' 秒' : '—' }}</small></article>
      <article class="overview-card"><p>24 小时未上报点位</p><strong>{{ operations?.no_report_points.length ?? '—' }}</strong><small>{{ operations?.no_report_points.map(item => item.code).join('、') || '无' }}</small></article>
    </div>
  </section>
</template>
