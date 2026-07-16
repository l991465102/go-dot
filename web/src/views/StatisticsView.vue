<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useProjectStore } from '@/stores/project'
import { completedDayRange, completedHourRange, getRealtime, getSummary, getTrend, type StatisticsSummary, type TrendPoint } from '@/api/statistics'
import TrendChart from '@/components/statistics/TrendChart.vue'

const projects = useProjectStore()
const project = computed(() => projects.currentProject)
const filters = reactive({ hours: 24, granularity: 'hour' as 'hour'|'day', domain: '', module: '', scene: '', point: '', platform: '', channel: '' })
const summary = ref<StatisticsSummary>(); const realtime = ref<StatisticsSummary>(); const trend = ref<TrendPoint[]>([])
const loading = ref(false); const error = ref(''); let controller: AbortController | undefined
async function load() {
  if (!project.value) return
  controller?.abort(); controller = new AbortController(); const signal = controller.signal
  loading.value = true; error.value = ''
  const range = filters.granularity === 'day' ? completedDayRange(Math.ceil(filters.hours / 24), project.value.timezone) : completedHourRange(filters.hours, project.value.timezone)
  const query = { projectId: project.value.id, ...range, timezone: project.value.timezone, granularity: filters.granularity, domainCategoryCode: filters.domain, moduleCategoryCode: filters.module, sceneCategoryCode: filters.scene, pointCodes: filters.point ? [filters.point.trim()] : undefined, platform: filters.platform, channel: filters.channel }
  try {
    const [total, series] = await Promise.all([getSummary(query, signal), getTrend(query, signal)])
    if (signal.aborted) return
    summary.value = total; trend.value = series.items
    realtime.value = filters.point ? await getRealtime({ ...query, granularity: 'hour', start: new Date(new Date(range.end).getTime() - 3_600_000).toISOString() }, signal) : undefined
  } catch (cause) { if (!signal.aborted) error.value = cause instanceof Error ? cause.message : '统计查询失败' }
  finally { if (!signal.aborted) loading.value = false }
}
watch(() => projects.currentProjectId, load, { immediate: true }); onBeforeUnmount(() => controller?.abort())
</script>
<template>
  <section aria-labelledby="statistics-title"><header class="page-heading"><div><p class="eyebrow">ANALYTICS</p><h1 id="statistics-title">统计分析</h1><p>ClickHouse 历史趋势约 1 分钟延迟；指定单一同口径点位后可查询 Redis 实时数据。</p></div></header>
    <el-form class="filter-panel" label-position="top" @submit.prevent="load">
      <el-form-item label="范围"><el-select v-model="filters.hours"><el-option label="24 小时" :value="24"/><el-option label="7 天" :value="168"/><el-option label="30 天" :value="720"/></el-select></el-form-item>
      <el-form-item label="粒度"><el-select v-model="filters.granularity"><el-option label="小时" value="hour"/><el-option label="天" value="day"/></el-select></el-form-item>
      <el-form-item label="业务域编码"><el-input v-model="filters.domain"/></el-form-item><el-form-item label="模块编码"><el-input v-model="filters.module"/></el-form-item><el-form-item label="场景编码"><el-input v-model="filters.scene"/></el-form-item>
      <el-form-item label="点位编码"><el-input v-model="filters.point" placeholder="实时查询必须填写"/></el-form-item><el-form-item label="平台"><el-input v-model="filters.platform"/></el-form-item><el-form-item label="渠道"><el-input v-model="filters.channel"/></el-form-item>
      <el-button native-type="submit" type="primary" :loading="loading">查询</el-button>
    </el-form>
    <div v-if="error" class="page-error" role="alert">{{ error }}</div>
    <div class="metric-card-grid"><article class="overview-card"><p>历史 PV</p><strong>{{ summary?.pv ?? '—' }}</strong><small>约 1 分钟延迟</small></article><article class="overview-card"><p>历史 UV{{ summary ? (summary.approximate ? '（近似）':'（精确）'):'' }}</p><strong>{{ summary?.uv ?? '—' }}</strong></article><article class="overview-card"><p>实时 PV</p><strong>{{ realtime?.pv ?? '—' }}</strong><small>{{ filters.point ? 'Redis':'请选择点位' }}</small></article><article class="overview-card"><p>实时 UV{{ realtime ? (realtime.approximate ? '（近似）':'（精确）'):'' }}</p><strong>{{ realtime?.uv ?? '—' }}</strong></article></div>
    <article class="overview-card"><TrendChart v-if="trend.length" :items="trend" :timezone="project?.timezone ?? 'UTC'"/><el-empty v-else-if="!loading" description="暂无统计数据"/></article>
  </section>
</template>
