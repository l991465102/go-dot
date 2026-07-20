<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useProjectStore } from '@/stores/project'
import {
  createReport, deleteReport, exportDaily, getReport, listReports, queryDaily, updateReport,
  type DailyResult, type ReportMutation, type ReportPoint, type SavedReport,
} from '@/api/reports'
import { listPoints } from '@/api/tracking'
import { formatShanghaiDateTime } from '@/utils/datetime'

const route = useRoute()
const projects = useProjectStore()
const projectId = computed(() => {
  const id = Number(route.params.projectId)
  return Number.isSafeInteger(id) && id > 0 ? id : 0
})
const canMutate = computed(() => projects.canMutate)

const reports = ref<SavedReport[]>([])
const selectedReportId = ref<number>()
const selectedReport = ref<SavedReport>()
const savedSnapshot = ref<SavedReport>()
const availablePoints = ref<ReportPoint[]>([])
const lastResult = ref<DailyResult>()
const loadingReports = ref(false)
const loadingReport = ref(false)
const loadingPoints = ref(false)
const querying = ref(false)
const exporting = ref(false)
const mutating = ref(false)
const error = ref('')
const success = ref('')
const confirmDelete = ref(false)
const editable = reactive({ name: '', description: '', defaultDays: 30, dateRange: [] as string[], points: [] as ReportPoint[] })

let projectController: AbortController | undefined
let reportController: AbortController | undefined
let pointsController: AbortController | undefined
let queryController: AbortController | undefined

function isCanceled(cause: unknown) {
  const name = (cause as { name?: string }).name
  return name === 'CanceledError' || name === 'AbortError'
}
function owns(active: AbortController, id: number, current: AbortController | undefined) {
  return current === active && !active.signal.aborted && projectId.value === id
}
function shanghaiToday(now = new Date()): string {
  const parts = new Intl.DateTimeFormat('en-CA', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit' }).formatToParts(now)
  const value = Object.fromEntries(parts.map((part) => [part.type, part.value]))
  return `${value.year}-${value.month}-${value.day}`
}
function shiftCivilDate(date: string, offset: number): string {
  const [year, month, day] = date.split('-').map(Number)
  return new Date(Date.UTC(year!, month! - 1, day! + offset)).toISOString().slice(0, 10)
}
function rangeForDays(days: number): string[] {
  const end = shanghaiToday()
  return [shiftCivilDate(end, -(Math.max(1, days) - 1)), end]
}
function resetEditable(days = 30) {
  editable.name = ''; editable.description = ''; editable.defaultDays = days
  editable.dateRange = rangeForDays(days); editable.points = []; lastResult.value = undefined
}
function snapshot(report: SavedReport): SavedReport {
  return { ...report, points: report.points?.map((point) => ({ ...point })) }
}
function mergePoints(...groups: ReportPoint[][]) {
  const merged = new Map<number, ReportPoint>()
  for (const point of editable.points) merged.set(point.id, point)
  for (const group of groups) for (const point of group) merged.set(point.id, point)
  availablePoints.value = [...merged.values()]
}
function reportPoint(point: Awaited<ReturnType<typeof listPoints>>['items'][number]): ReportPoint {
  return { id: point.id, code: point.code, name: point.name, definition_state: point.definition_state, uv_mode: point.uv_mode, sort_no: 0 }
}

async function searchPoints(query = '') {
  pointsController?.abort()
  const active = new AbortController(); pointsController = active
  const id = projectId.value
  if (!id) return
  loadingPoints.value = true
  try {
    const filter = { q: query || undefined, page: 1, page_size: 100 }
    const [published, deprecated] = await Promise.all([
      listPoints(id, { ...filter, definition_state: 'PUBLISHED' }, active.signal),
      listPoints(id, { ...filter, definition_state: 'DEPRECATED' }, active.signal),
    ])
    if (!owns(active, id, pointsController)) return
    mergePoints(published.items.map(reportPoint), deprecated.items.map(reportPoint))
  } catch (cause) {
    if (owns(active, id, pointsController) && !isCanceled(cause)) error.value = cause instanceof Error ? cause.message : '点位加载失败'
  } finally {
    if (owns(active, id, pointsController)) loadingPoints.value = false
  }
}

async function loadProject() {
  projectController?.abort(); reportController?.abort(); pointsController?.abort(); queryController?.abort()
  const active = new AbortController(); projectController = active
  const id = projectId.value
  reports.value = []; selectedReportId.value = undefined; selectedReport.value = undefined; savedSnapshot.value = undefined; availablePoints.value = []
  resetEditable(); error.value = ''; success.value = ''
  if (!id) return
  loadingReports.value = true
  void searchPoints()
  try {
    const items = await listReports(id, active.signal)
    if (owns(active, id, projectController)) reports.value = items
  } catch (cause) {
    if (owns(active, id, projectController) && !isCanceled(cause)) error.value = cause instanceof Error ? cause.message : '报表列表加载失败'
  } finally {
    if (owns(active, id, projectController)) loadingReports.value = false
  }
}

function applySavedReport(report: SavedReport) {
  const next = snapshot(report)
  selectedReport.value = next; savedSnapshot.value = snapshot(report)
  editable.name = next.name; editable.description = next.description; editable.defaultDays = next.default_days
  editable.dateRange = rangeForDays(next.default_days)
  editable.points = [...(next.points ?? [])].sort((left, right) => left.sort_no - right.sort_no).map((point) => ({ ...point }))
  mergePoints(editable.points); lastResult.value = undefined
}
async function loadSavedReport(value: number | undefined) {
  reportController?.abort()
  if (!value) return
  const active = new AbortController(); reportController = active
  const id = projectId.value
  loadingReport.value = true; error.value = ''
  try {
    const report = await getReport(id, value, active.signal)
    if (owns(active, id, reportController) && selectedReportId.value === value) applySavedReport(report)
  } catch (cause) {
    if (owns(active, id, reportController) && !isCanceled(cause)) error.value = cause instanceof Error ? cause.message : '报表加载失败'
  } finally {
    if (owns(active, id, reportController)) loadingReport.value = false
  }
}

function selectPointIds(value: unknown) {
  let ids = Array.isArray(value) ? value.map(Number).filter(Number.isSafeInteger) : []
  if (ids.length > 100) { error.value = '最多选择 100 个点位'; ids = ids.slice(0, 100) }
  const byId = new Map(availablePoints.value.map((point) => [point.id, point]))
  editable.points = ids.flatMap((id) => byId.get(id) ? [{ ...byId.get(id)! }] : [])
}
function movePoint(index: number, offset: number) {
  const target = index + offset
  if (target < 0 || target >= editable.points.length) return
  const reordered = [...editable.points]
  ;[reordered[index], reordered[target]] = [reordered[target]!, reordered[index]!]
  editable.points = reordered
}
function removePoint(id: number) { editable.points = editable.points.filter((point) => point.id !== id) }
function newReport() {
  reportController?.abort(); selectedReportId.value = undefined; selectedReport.value = undefined; savedSnapshot.value = undefined
  resetEditable(); error.value = ''; success.value = ''
}
function validQuery() {
  if (editable.dateRange.length !== 2 || !editable.dateRange[0] || !editable.dateRange[1]) { error.value = '请选择完整日期范围'; return false }
  if (!editable.points.length) { error.value = '请至少选择一个点位'; return false }
  return true
}
function mutation(): ReportMutation | undefined {
  if (!editable.name.trim()) { error.value = '请输入报表名称'; return }
  if (!validQuery()) return
  return {
    name: editable.name.trim(), description: editable.description.trim(),
    default_days: Math.max(1, Math.min(365, Number(editable.defaultDays) || 30)),
    point_ids: editable.points.map((point) => point.id),
  }
}
function replaceReport(report: SavedReport) {
  const normalized = snapshot({ ...report, points: report.points ?? editable.points.map((point, index) => ({ ...point, sort_no: index })) })
  const index = reports.value.findIndex((item) => item.id === normalized.id)
  if (index === -1) reports.value.push(normalized); else reports.value[index] = normalized
  selectedReportId.value = normalized.id
  selectedReport.value = snapshot(normalized)
  savedSnapshot.value = snapshot(normalized)
}

async function saveAsReport() {
  if (!canMutate.value || mutating.value) return
  const input = mutation(); if (!input) return
  mutating.value = true; error.value = ''
  const id = projectId.value
  try { const created = await createReport(id, input); if (projectId.value === id) { replaceReport(created); success.value = '报表已创建' } }
  catch (cause) { if (projectId.value === id) error.value = cause instanceof Error ? cause.message : '报表创建失败' }
  finally { mutating.value = false }
}
async function saveReport() {
  const base = savedSnapshot.value
  if (!canMutate.value || !base || mutating.value) { if (!base) error.value = '请先选择已保存报表，或使用另存为'; return }
  const input = mutation(); if (!input) return
  const id = projectId.value
  mutating.value = true; error.value = ''
  try {
    const updated = await updateReport(id, base.id, { ...input, expected_updated_at: base.updated_at })
    if (projectId.value === id) { replaceReport(updated); success.value = '报表配置已保存' }
  } catch (cause) { if (projectId.value === id) error.value = cause instanceof Error ? cause.message : '报表保存失败' }
  finally { mutating.value = false }
}
async function confirmReportDeletion() {
  const base = savedSnapshot.value
  if (!canMutate.value || !base || mutating.value) return
  const id = projectId.value
  mutating.value = true; error.value = ''
  try { await deleteReport(id, base.id); if (projectId.value === id) { reports.value = reports.value.filter((item) => item.id !== base.id); confirmDelete.value = false; newReport(); success.value = '报表已删除' } }
  catch (cause) { if (projectId.value === id) error.value = cause instanceof Error ? cause.message : '报表删除失败' }
  finally { mutating.value = false }
}
async function runQuery() {
  if (!validQuery()) return
  queryController?.abort()
  const active = new AbortController(); queryController = active
  const id = projectId.value
  querying.value = true; error.value = ''
  try {
    const result = await queryDaily({ project_id: id, start_date: editable.dateRange[0]!, end_date: editable.dateRange[1]!, point_ids: editable.points.map((point) => point.id) }, active.signal)
    if (owns(active, id, queryController)) lastResult.value = result
  } catch (cause) {
    if (owns(active, id, queryController) && !isCanceled(cause)) error.value = cause instanceof Error ? cause.message : '报表查询失败'
  } finally { if (owns(active, id, queryController)) querying.value = false }
}
async function download() {
  if (!validQuery() || exporting.value) return
  exporting.value = true; error.value = ''
  const id = projectId.value
  try {
    const { blob, filename } = await exportDaily({
      project_id: id, start_date: editable.dateRange[0]!, end_date: editable.dateRange[1]!, point_ids: editable.points.map((point) => point.id),
      report_name: editable.name.trim(), report_description: editable.description.trim(),
    })
    if (projectId.value !== id) return
    const url = URL.createObjectURL(blob)
    try { const anchor = document.createElement('a'); anchor.href = url; anchor.download = filename; anchor.click() }
    finally { URL.revokeObjectURL(url) }
  } catch (cause) { if (projectId.value === id) error.value = cause instanceof Error ? cause.message : '报表导出失败' }
  finally { exporting.value = false }
}

watch(projectId, loadProject, { immediate: true })
watch(selectedReportId, (value) => { if (value && value !== selectedReport.value?.id) void loadSavedReport(value) })
onBeforeUnmount(() => { projectController?.abort(); reportController?.abort(); pointsController?.abort(); queryController?.abort() })
</script>

<template>
  <section aria-labelledby="custom-reports-title">
    <header class="page-heading">
      <div><p class="eyebrow">CUSTOM REPORTS</p><h1 id="custom-reports-title">自定义报表</h1><p>按自然日查看点位 PV 与 UV。统计时区：Asia/Shanghai</p></div>
      <div v-if="canMutate" class="heading-actions">
        <el-button data-test="new-report" @click="newReport">新建</el-button>
        <el-button data-test="save-report" :disabled="!savedSnapshot" :loading="mutating" @click="saveReport">保存配置</el-button>
        <el-button data-test="save-as-report" :loading="mutating" @click="saveAsReport">另存为</el-button>
        <el-button data-test="delete-report" type="danger" :disabled="!savedSnapshot" @click="confirmDelete = true">删除</el-button>
      </div>
    </header>
    <div v-if="error" class="page-error" role="alert">{{ error }}</div>
    <el-alert v-if="success" :title="success" type="success" :closable="false" />

    <section class="report-config-panel" aria-labelledby="report-config-title" v-loading="loadingReports || loadingReport">
      <h2 id="report-config-title">报表配置</h2>
      <el-form label-position="top" class="report-config-grid" @submit.prevent="runQuery">
        <el-form-item label="已保存报表">
          <el-select v-model="selectedReportId" data-test="report-select" clearable placeholder="选择报表" aria-label="已保存报表">
            <el-option v-for="report in reports" :key="report.id" :label="report.name" :value="report.id" />
          </el-select>
          <small v-if="savedSnapshot" class="report-meta">上次更新：{{ formatShanghaiDateTime(savedSnapshot.updated_at) }}</small>
        </el-form-item>
        <el-form-item label="报表名称"><el-input v-model="editable.name" data-test="report-name" aria-label="报表名称" :readonly="!canMutate" /></el-form-item>
        <el-form-item label="报表说明"><el-input v-model="editable.description" data-test="report-description" aria-label="报表说明" :readonly="!canMutate" /></el-form-item>
        <el-form-item label="默认天数"><input v-model.number="editable.defaultDays" data-test="default-days" aria-label="默认天数" type="number" min="1" max="365" :readonly="!canMutate"></el-form-item>
        <el-form-item label="日期范围（Asia/Shanghai）">
          <el-date-picker v-model="editable.dateRange" data-test="date-range" aria-label="日期范围" type="daterange" value-format="YYYY-MM-DD" start-placeholder="开始日期" end-placeholder="结束日期" />
        </el-form-item>
        <el-form-item label="点位（最多 100 个）" class="report-point-picker">
          <el-select :model-value="editable.points.map((point) => point.id)" data-test="point-select" multiple filterable remote reserve-keyword :remote-method="searchPoints" :loading="loadingPoints" aria-label="点位选择" placeholder="输入编码或名称搜索" @update:model-value="selectPointIds">
            <el-option v-for="point in availablePoints" :key="point.id" :label="`${point.name}（${point.code}）`" :value="point.id" />
          </el-select>
        </el-form-item>
      </el-form>
      <div class="ordered-points" aria-label="已选点位顺序">
        <div v-if="!editable.points.length" class="empty-state">尚未选择点位</div>
        <div v-for="(point, index) in editable.points" :key="point.id" class="ordered-point-row">
          <span class="ordered-point-index">{{ index + 1 }}</span><span><strong>{{ point.name }}</strong><small>{{ point.code }} · {{ point.definition_state }}</small></span>
          <div class="ordered-point-actions">
            <el-button :data-test="`move-point-up-${point.id}`" aria-label="上移点位" :disabled="index === 0" @click="movePoint(index, -1)">↑</el-button>
            <el-button :data-test="`move-point-down-${point.id}`" aria-label="下移点位" :disabled="index === editable.points.length - 1" @click="movePoint(index, 1)">↓</el-button>
            <el-button :data-test="`remove-point-${point.id}`" aria-label="移除点位" @click="removePoint(point.id)">移除</el-button>
          </div>
        </div>
      </div>
      <footer class="report-actions"><el-button data-test="query-report" type="primary" :loading="querying" @click="runQuery">查询</el-button><el-button data-test="export-report" :loading="exporting" @click="download">导出 Excel</el-button></footer>
    </section>

    <section class="report-result-panel" aria-labelledby="report-result-title">
      <h2 id="report-result-title">查询结果</h2>
      <template v-if="lastResult">
        <p class="report-timezone">统计时区：{{ lastResult.timezone }}</p>
        <div class="report-summary-grid"><el-card><span>点位数</span><strong>{{ lastResult.point_count }}</strong></el-card><el-card><span>天数</span><strong>{{ lastResult.day_count }}</strong></el-card><el-card><span>总 PV</span><strong>{{ lastResult.total_pv }}</strong></el-card></div>
        <el-table :data="lastResult.items" empty-text="暂无统计数据">
          <el-table-column prop="date" label="日期" min-width="120" />
          <el-table-column label="状态" min-width="100"><template #default="scope"><el-tag :type="scope.row.complete ? 'success' : 'warning'">{{ scope.row.complete ? '已完成' : '未完整' }}</el-tag></template></el-table-column>
          <el-table-column prop="point_code" label="点位编码" min-width="160" /><el-table-column prop="point_name" label="点位名称" min-width="160" />
          <el-table-column prop="pv" label="PV" min-width="90" /><el-table-column prop="uv" label="UV" min-width="90" /><el-table-column prop="uv_mode" label="UV 模式" min-width="130" />
        </el-table>
      </template>
      <el-empty v-else description="配置日期和点位后开始查询" />
    </section>

    <el-dialog v-model="confirmDelete" title="删除报表" width="420px">
      <p>确定删除“{{ savedSnapshot?.name }}”吗？此操作无法撤销。</p>
      <template #footer><el-button data-test="cancel-delete-report" @click="confirmDelete = false">取消</el-button><el-button data-test="confirm-delete-report" type="danger" :loading="mutating" @click="confirmReportDeletion">确认删除</el-button></template>
    </el-dialog>
  </section>
</template>
