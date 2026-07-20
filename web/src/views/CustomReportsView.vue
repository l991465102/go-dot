<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useProjectStore } from '@/stores/project'
import {
  createReport, deleteReport, exportDaily, getReport, listReports, queryDaily, updateReport,
  type DailyResult, type ReportMutation, type ReportPoint, type SavedReport,
} from '@/api/reports'
import { listCategories, listPoints, type Category } from '@/api/tracking'
import { formatShanghaiDateTime } from '@/utils/datetime'

const route = useRoute()
const projects = useProjectStore()
const projectId = computed(() => {
  const id = Number(route.params.projectId)
  return Number.isSafeInteger(id) && id > 0 ? id : 0
})
const routeProject = computed(() => projects.projects.find((project) => project.id === projectId.value))
const canMutate = computed(() => routeProject.value?.role === 'ADMIN')

const reports = ref<SavedReport[]>([])
const categories = ref<Category[]>([])
const selectedReportId = ref<number>()
const selectedReport = ref<SavedReport>()
const savedSnapshot = ref<SavedReport>()
const availablePoints = ref<ReportPoint[]>([])
const pointResults = new Map<number, ReportPoint>()
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
const searchQuery = ref('')
const categoryId = ref<number>()
const pointPage = ref(0)
const publishedTotal = ref(0)
const deprecatedTotal = ref(0)
const publishedLoaded = ref(0)
const deprecatedLoaded = ref(0)
const editable = reactive({
  name: '', description: '', defaultDays: 30,
  dateRange: null as string[] | null,
  points: [] as ReportPoint[],
})

const hasMorePoints = computed(() => pointPage.value > 0 && (
  publishedLoaded.value < publishedTotal.value || deprecatedLoaded.value < deprecatedTotal.value
))

let projectController: AbortController | undefined
let reportController: AbortController | undefined
let pointsController: AbortController | undefined
let queryController: AbortController | undefined
let contextEpoch = 0
let sequence = 0
let mutationToken = 0
let exportToken = 0
let suppressSelectionWatch = false

function isCanceled(cause: unknown) {
  const name = (cause as { name?: string }).name
  return name === 'CanceledError' || name === 'AbortError'
}
function owns(active: AbortController, epoch: number, id: number, current: AbortController | undefined) {
  return current === active && !active.signal.aborted && contextEpoch === epoch && projectId.value === id
}
function operationOwns(token: number, epoch: number, id: number, kind: 'mutation' | 'export') {
  return contextEpoch === epoch && projectId.value === id && (kind === 'mutation' ? mutationToken : exportToken) === token
}
function userError(cause: unknown, fallback: string) {
  const response = (cause as { response?: { status?: number; data?: { message?: unknown } } }).response
  const backend = response?.data?.message
  const detail = typeof backend === 'string' && backend.trim() ? `：${backend.trim()}` : ''
  if (response?.status === 409) return `配置已被其他人更新，请重新加载后再保存${detail}`
  if (response?.status === 400) return `请求参数无效，请检查日期、默认天数和点位配置${detail}`
  if (detail) return detail.slice(1)
  return cause instanceof Error && cause.message ? cause.message : fallback
}
function shanghaiToday(now = new Date()): string {
  const parts = new Intl.DateTimeFormat('en-CA', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit' }).formatToParts(now)
  const value = Object.fromEntries(parts.map((part) => [part.type, part.value]))
  return `${value.year}-${value.month}-${value.day}`
}
function civilMillis(value: string): number | undefined {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return
  const [year, month, day] = value.split('-').map(Number)
  const milliseconds = Date.UTC(year!, month! - 1, day!)
  const roundTrip = new Date(milliseconds).toISOString().slice(0, 10)
  return roundTrip === value ? milliseconds : undefined
}
function shiftCivilDate(date: string, offset: number): string {
  return new Date(civilMillis(date)! + offset * 86_400_000).toISOString().slice(0, 10)
}
function rangeForDays(days: number): string[] {
  const end = shanghaiToday()
  return [shiftCivilDate(end, -(days - 1)), end]
}
function validateDateRange(): { start: string; end: string } | undefined {
  if (!projectId.value) { error.value = '项目无效，请重新选择项目'; return }
  const range = editable.dateRange
  if (!range || range.length !== 2 || !range[0] || !range[1]) { error.value = '请选择完整日期范围'; return }
  const startMs = civilMillis(range[0]); const endMs = civilMillis(range[1])
  if (startMs === undefined || endMs === undefined) { error.value = '日期格式无效，请重新选择日期'; return }
  if (startMs > endMs) { error.value = '开始日期不能晚于结束日期'; return }
  if (range[1] > shanghaiToday()) { error.value = '结束日期不能晚于上海当天'; return }
  const days = Math.floor((endMs - startMs) / 86_400_000) + 1
  if (days < 1 || days > 90) { error.value = '日期范围最多为 90 个自然日'; return }
  return { start: range[0], end: range[1] }
}
function validQuery() {
  const range = validateDateRange()
  if (!range) return
  if (!editable.points.length) { error.value = '请至少选择一个点位'; return }
  if (editable.points.length > 100) { error.value = '最多选择 100 个点位'; return }
  return range
}
function mutation(): ReportMutation | undefined {
  if (!editable.name.trim()) { error.value = '请输入报表名称'; return }
  const days = Number(editable.defaultDays)
  if (!Number.isInteger(days) || days < 1 || days > 90) { error.value = '默认天数必须是 1 至 90 的整数'; return }
  if (!validQuery()) return
  return { name: editable.name.trim(), description: editable.description.trim(), default_days: days, point_ids: editable.points.map((point) => point.id) }
}
function snapshot(report: SavedReport): SavedReport {
  return { ...report, points: report.points?.map((point) => ({ ...point })) }
}
function resetEditable(days = 30) {
  editable.name = ''; editable.description = ''; editable.defaultDays = days
  editable.dateRange = rangeForDays(days); editable.points = []; lastResult.value = undefined
}
function setSelectedReportId(value: number | undefined) {
  suppressSelectionWatch = true
  selectedReportId.value = value
  suppressSelectionWatch = false
}
function abortContextRequests(includeProject = false) {
  if (includeProject) projectController?.abort()
  reportController?.abort(); pointsController?.abort(); queryController?.abort()
}
function invalidateContext(includeProject = false) {
  contextEpoch += 1
  mutationToken = ++sequence; exportToken = ++sequence
  abortContextRequests(includeProject)
  loadingReport.value = false; loadingPoints.value = false; querying.value = false; exporting.value = false; mutating.value = false
  confirmDelete.value = false
}
function rebuildAvailablePoints() {
  const merged = new Map<number, ReportPoint>()
  for (const point of editable.points) merged.set(point.id, point)
  for (const point of pointResults.values()) if (!merged.has(point.id)) merged.set(point.id, point)
  availablePoints.value = [...merged.values()]
}
function reportPoint(point: Awaited<ReturnType<typeof listPoints>>['items'][number]): ReportPoint {
  return { id: point.id, code: point.code, name: point.name, definition_state: point.definition_state, uv_mode: point.uv_mode, sort_no: 0 }
}

async function loadPointPage(reset = false) {
  pointsController?.abort()
  const active = new AbortController(); pointsController = active
  const id = projectId.value; const epoch = contextEpoch
  if (!id) return
  if (reset) {
    pointResults.clear(); pointPage.value = 0
    publishedTotal.value = 0; deprecatedTotal.value = 0; publishedLoaded.value = 0; deprecatedLoaded.value = 0
    rebuildAvailablePoints()
  }
  const page = pointPage.value + 1
  loadingPoints.value = true; error.value = ''
  try {
    const filter = { q: searchQuery.value || undefined, category_id: categoryId.value, page, page_size: 50 }
    const [published, deprecated] = await Promise.all([
      listPoints(id, { ...filter, definition_state: 'PUBLISHED' }, active.signal),
      listPoints(id, { ...filter, definition_state: 'DEPRECATED' }, active.signal),
    ])
    if (!owns(active, epoch, id, pointsController)) return
    pointPage.value = page
    publishedTotal.value = published.total; deprecatedTotal.value = deprecated.total
    publishedLoaded.value += published.items.length; deprecatedLoaded.value += deprecated.items.length
    for (const item of [...published.items, ...deprecated.items]) {
      if (!pointResults.has(item.id)) pointResults.set(item.id, reportPoint(item))
    }
    rebuildAvailablePoints()
  } catch (cause) {
    if (owns(active, epoch, id, pointsController) && !isCanceled(cause)) error.value = userError(cause, '点位加载失败')
  } finally {
    if (owns(active, epoch, id, pointsController)) loadingPoints.value = false
  }
}
function searchPoints(query = '') { searchQuery.value = query; void loadPointPage(true) }

async function loadProject() {
  invalidateContext(true)
  const active = new AbortController(); projectController = active
  const id = projectId.value; const epoch = contextEpoch
  reports.value = []; categories.value = []; setSelectedReportId(undefined); selectedReport.value = undefined; savedSnapshot.value = undefined
  availablePoints.value = []; pointResults.clear(); searchQuery.value = ''; categoryId.value = undefined
  loadingReports.value = false; success.value = ''; error.value = ''; resetEditable()
  if (!id) return
  loadingReports.value = true
  void loadPointPage(true)
  const [reportResult, categoryResult] = await Promise.allSettled([listReports(id, active.signal), listCategories(id, active.signal)])
  if (!owns(active, epoch, id, projectController)) return
  if (reportResult.status === 'fulfilled') reports.value = reportResult.value
  else if (!isCanceled(reportResult.reason)) error.value = userError(reportResult.reason, '报表列表加载失败')
  if (categoryResult.status === 'fulfilled') categories.value = categoryResult.value
  else if (!isCanceled(categoryResult.reason) && !error.value) error.value = userError(categoryResult.reason, '业务分类加载失败')
  loadingReports.value = false
}

function applySavedReport(report: SavedReport) {
  const next = snapshot(report)
  selectedReport.value = next; savedSnapshot.value = snapshot(report)
  editable.name = next.name; editable.description = next.description; editable.defaultDays = next.default_days
  editable.dateRange = rangeForDays(next.default_days)
  editable.points = [...(next.points ?? [])].sort((a, b) => a.sort_no - b.sort_no).map((point) => ({ ...point }))
  rebuildAvailablePoints(); lastResult.value = undefined
}
function enterTemporaryMode() {
  invalidateContext()
  setSelectedReportId(undefined); selectedReport.value = undefined; savedSnapshot.value = undefined
  success.value = ''; error.value = ''; resetEditable(); rebuildAvailablePoints()
}
async function selectSavedReport(value: number) {
  invalidateContext()
  const active = new AbortController(); reportController = active
  const id = projectId.value; const epoch = contextEpoch
  selectedReport.value = undefined; savedSnapshot.value = undefined; lastResult.value = undefined
  success.value = ''; error.value = ''; loadingReport.value = true
  try {
    const report = await getReport(id, value, active.signal)
    if (owns(active, epoch, id, reportController) && selectedReportId.value === value) applySavedReport(report)
  } catch (cause) {
    if (owns(active, epoch, id, reportController) && !isCanceled(cause)) {
      setSelectedReportId(undefined); selectedReport.value = undefined; savedSnapshot.value = undefined; resetEditable(); rebuildAvailablePoints()
      error.value = userError(cause, '报表加载失败')
    }
  } finally {
    if (owns(active, epoch, id, reportController)) loadingReport.value = false
  }
}

function selectPointIds(value: unknown) {
  let ids = Array.isArray(value) ? value.map(Number).filter(Number.isSafeInteger) : []
  if (ids.length > 100) { error.value = '最多选择 100 个点位'; ids = ids.slice(0, 100) }
  const byId = new Map(availablePoints.value.map((point) => [point.id, point]))
  editable.points = ids.flatMap((id) => byId.has(id) ? [{ ...byId.get(id)! }] : [])
  rebuildAvailablePoints()
}
function movePoint(index: number, offset: number) {
  const target = index + offset
  if (target < 0 || target >= editable.points.length) return
  const next = [...editable.points]; [next[index], next[target]] = [next[target]!, next[index]!]; editable.points = next
}
function removePoint(id: number) { editable.points = editable.points.filter((point) => point.id !== id); rebuildAvailablePoints() }

function replaceReport(report: SavedReport) {
  const normalized = snapshot({ ...report, points: report.points ?? editable.points.map((point, index) => ({ ...point, sort_no: index })) })
  const index = reports.value.findIndex((item) => item.id === normalized.id)
  if (index === -1) reports.value.push(normalized); else reports.value[index] = normalized
  setSelectedReportId(normalized.id); selectedReport.value = snapshot(normalized); savedSnapshot.value = snapshot(normalized)
}
async function saveAsReport() {
  if (!canMutate.value || mutating.value) return
  const input = mutation(); if (!input) return
  const id = projectId.value; const epoch = contextEpoch; const token = ++sequence; mutationToken = token
  mutating.value = true; success.value = ''; error.value = ''
  try {
    const created = await createReport(id, input)
    if (operationOwns(token, epoch, id, 'mutation')) { replaceReport(created); success.value = '报表已创建' }
  } catch (cause) { if (operationOwns(token, epoch, id, 'mutation')) error.value = userError(cause, '报表创建失败') }
  finally { if (operationOwns(token, epoch, id, 'mutation')) mutating.value = false }
}
async function saveReport() {
  const base = savedSnapshot.value
  if (!canMutate.value || !base || mutating.value) { if (!base) error.value = '请先选择已保存报表，或使用另存为'; return }
  const input = mutation(); if (!input) return
  const id = projectId.value; const epoch = contextEpoch; const reportId = base.id; const token = ++sequence; mutationToken = token
  mutating.value = true; success.value = ''; error.value = ''
  try {
    const updated = await updateReport(id, reportId, { ...input, expected_updated_at: base.updated_at })
    if (operationOwns(token, epoch, id, 'mutation') && savedSnapshot.value?.id === reportId) { replaceReport(updated); success.value = '报表配置已保存' }
  } catch (cause) { if (operationOwns(token, epoch, id, 'mutation') && savedSnapshot.value?.id === reportId) error.value = userError(cause, '报表保存失败') }
  finally { if (operationOwns(token, epoch, id, 'mutation')) mutating.value = false }
}
async function confirmReportDeletion() {
  const base = savedSnapshot.value
  if (!canMutate.value || !base || mutating.value) return
  const id = projectId.value; const epoch = contextEpoch; const reportId = base.id; const token = ++sequence; mutationToken = token
  mutating.value = true; success.value = ''; error.value = ''
  try {
    await deleteReport(id, reportId)
    if (operationOwns(token, epoch, id, 'mutation') && savedSnapshot.value?.id === reportId) {
      reports.value = reports.value.filter((item) => item.id !== reportId); enterTemporaryMode(); success.value = '报表已删除'
    }
  } catch (cause) { if (operationOwns(token, epoch, id, 'mutation') && savedSnapshot.value?.id === reportId) error.value = userError(cause, '报表删除失败') }
  finally { if (operationOwns(token, epoch, id, 'mutation')) mutating.value = false }
}
async function runQuery() {
  const range = validQuery(); if (!range) return
  queryController?.abort()
  const active = new AbortController(); queryController = active
  const id = projectId.value; const epoch = contextEpoch
  querying.value = true; success.value = ''; error.value = ''
  try {
    const result = await queryDaily({ project_id: id, start_date: range.start, end_date: range.end, point_ids: editable.points.map((point) => point.id) }, active.signal)
    if (owns(active, epoch, id, queryController)) lastResult.value = result
  } catch (cause) { if (owns(active, epoch, id, queryController) && !isCanceled(cause)) error.value = userError(cause, '报表查询失败') }
  finally { if (owns(active, epoch, id, queryController)) querying.value = false }
}
async function download() {
  const range = validQuery(); if (!range || exporting.value) return
  const id = projectId.value; const epoch = contextEpoch; const token = ++sequence; exportToken = token
  exporting.value = true; success.value = ''; error.value = ''
  try {
    const { blob, filename } = await exportDaily({ project_id: id, start_date: range.start, end_date: range.end, point_ids: editable.points.map((point) => point.id), report_name: editable.name.trim(), report_description: editable.description.trim() })
    if (!operationOwns(token, epoch, id, 'export')) return
    const url = URL.createObjectURL(blob)
    try { const anchor = document.createElement('a'); anchor.href = url; anchor.download = filename; anchor.click() }
    finally { URL.revokeObjectURL(url) }
  } catch (cause) { if (operationOwns(token, epoch, id, 'export')) error.value = userError(cause, '报表导出失败') }
  finally { if (operationOwns(token, epoch, id, 'export')) exporting.value = false }
}

watch(projectId, loadProject, { immediate: true })
watch(selectedReportId, (value) => {
  if (suppressSelectionWatch) return
  if (value !== undefined && value === selectedReport.value?.id) return
  if (value === undefined || value === null) enterTemporaryMode(); else void selectSavedReport(value)
}, { flush: 'sync' })
watch(categoryId, () => { void loadPointPage(true) })
watch(() => editable.dateRange?.join('|') ?? '', () => { lastResult.value = undefined })
watch(() => editable.points.map((point) => point.id).join(','), () => { lastResult.value = undefined })
onBeforeUnmount(() => { invalidateContext(true) })
</script>

<template>
  <section aria-labelledby="custom-reports-title">
    <header class="page-heading">
      <div><p class="eyebrow">CUSTOM REPORTS</p><h1 id="custom-reports-title">自定义报表</h1><p>按自然日查看点位 PV 与 UV。统计时区：Asia/Shanghai</p></div>
      <div v-if="canMutate" class="heading-actions">
        <el-button data-test="new-report" :disabled="mutating" @click="enterTemporaryMode">新建</el-button>
        <el-button data-test="save-report" :disabled="!savedSnapshot || loadingReport || mutating" :loading="mutating" @click="saveReport">保存配置</el-button>
        <el-button data-test="save-as-report" :disabled="loadingReport || mutating" :loading="mutating" @click="saveAsReport">另存为</el-button>
        <el-button data-test="delete-report" type="danger" :disabled="!savedSnapshot || loadingReport || mutating" @click="confirmDelete = true">删除</el-button>
      </div>
    </header>
    <div v-if="error" class="page-error" role="alert">{{ error }}</div>
    <el-alert v-if="success" :title="success" type="success" :closable="false" />

    <section class="report-config-panel" aria-labelledby="report-config-title" v-loading="loadingReports || loadingReport">
      <h2 id="report-config-title">报表配置</h2>
      <el-form label-position="top" class="report-config-grid" @submit.prevent="runQuery">
        <el-form-item label="已保存报表">
          <el-select v-model="selectedReportId" data-test="report-select" clearable :disabled="mutating" placeholder="选择报表" aria-label="已保存报表">
            <el-option v-for="report in reports" :key="report.id" :label="report.name" :value="report.id" />
          </el-select>
          <small v-if="savedSnapshot" class="report-meta">上次更新：{{ formatShanghaiDateTime(savedSnapshot.updated_at) }}</small>
        </el-form-item>
        <el-form-item label="报表名称"><el-input v-model="editable.name" data-test="report-name" aria-label="报表名称" :readonly="!canMutate" /></el-form-item>
        <el-form-item label="报表说明"><el-input v-model="editable.description" data-test="report-description" aria-label="报表说明" :readonly="!canMutate" /></el-form-item>
        <el-form-item label="默认天数"><input v-model.number="editable.defaultDays" data-test="default-days" aria-label="默认天数" type="number" min="1" max="90" :readonly="!canMutate"></el-form-item>
        <el-form-item label="日期范围（Asia/Shanghai）"><el-date-picker v-model="editable.dateRange" data-test="date-range" aria-label="日期范围" type="daterange" value-format="YYYY-MM-DD" start-placeholder="开始日期" end-placeholder="结束日期" /></el-form-item>
        <el-form-item label="业务分类"><el-select v-model="categoryId" data-test="category-select" clearable aria-label="业务分类"><el-option v-for="category in categories" :key="category.id" :label="category.name" :value="category.id" /></el-select></el-form-item>
        <el-form-item label="点位（最多 100 个）" class="report-point-picker">
          <el-select :model-value="editable.points.map((point) => point.id)" data-test="point-select" multiple filterable remote reserve-keyword :remote-method="searchPoints" :loading="loadingPoints" aria-label="点位选择" placeholder="按编码、名称或业务分类搜索" @update:model-value="selectPointIds">
            <el-option v-for="point in availablePoints" :key="point.id" :data-test="`point-option-${point.id}`" :label="`${point.name}（${point.code}）`" :value="point.id" />
          </el-select>
          <el-button v-if="hasMorePoints" data-test="load-more-points" :loading="loadingPoints" @click="loadPointPage(false)">加载更多点位</el-button>
        </el-form-item>
      </el-form>
      <div class="ordered-points" aria-label="已选点位顺序">
        <div v-if="!editable.points.length" class="empty-state">尚未选择点位</div>
        <div v-for="(point, index) in editable.points" :key="point.id" class="ordered-point-row">
          <span class="ordered-point-index">{{ index + 1 }}</span><span><strong>{{ point.name }}</strong><small>{{ point.code }} · {{ point.definition_state }}</small></span>
          <div class="ordered-point-actions">
            <el-button :data-test="`move-point-up-${point.id}`" :aria-label="`上移点位 ${point.name}`" :disabled="index === 0" @click="movePoint(index, -1)">↑</el-button>
            <el-button :data-test="`move-point-down-${point.id}`" :aria-label="`下移点位 ${point.name}`" :disabled="index === editable.points.length - 1" @click="movePoint(index, 1)">↓</el-button>
            <el-button :data-test="`remove-point-${point.id}`" :aria-label="`移除点位 ${point.name}`" @click="removePoint(point.id)">移除</el-button>
          </div>
        </div>
      </div>
      <footer class="report-actions"><el-button data-test="query-report" type="primary" :loading="querying" :disabled="mutating" @click="runQuery">查询</el-button><el-button data-test="export-report" :loading="exporting" :disabled="mutating" @click="download">导出 Excel</el-button></footer>
    </section>

    <section class="report-result-panel" aria-labelledby="report-result-title" v-loading="querying">
      <h2 id="report-result-title">查询结果</h2>
      <template v-if="lastResult">
        <p class="report-timezone">统计时区：{{ lastResult.timezone }}</p>
        <div class="report-summary-grid"><el-card><span>点位数</span><strong>{{ lastResult.point_count }}</strong></el-card><el-card><span>天数</span><strong>{{ lastResult.day_count }}</strong></el-card><el-card><span>总 PV</span><strong>{{ lastResult.total_pv }}</strong></el-card></div>
        <el-table :data="lastResult.items" empty-text="暂无统计数据">
          <el-table-column prop="date" label="日期" min-width="120" />
          <el-table-column label="状态" min-width="100"><template #default="scope"><el-tag :data-test="`completion-${scope.row.date}-${scope.row.point_id}`" :type="scope.row.complete ? 'success' : 'warning'">{{ scope.row.complete ? '已完成' : '未完整' }}</el-tag></template></el-table-column>
          <el-table-column prop="point_code" label="点位编码" min-width="160" /><el-table-column prop="point_name" label="点位名称" min-width="160" /><el-table-column prop="pv" label="PV" min-width="90" /><el-table-column prop="uv" label="UV" min-width="90" /><el-table-column prop="uv_mode" label="UV 模式" min-width="130" />
        </el-table>
      </template>
      <el-empty v-else description="配置日期和点位后开始查询" />
    </section>

    <el-dialog v-model="confirmDelete" title="删除报表" width="420px"><p>确定删除“{{ savedSnapshot?.name }}”吗？此操作无法撤销。</p><template #footer><el-button data-test="cancel-delete-report" :disabled="mutating" @click="confirmDelete = false">取消</el-button><el-button data-test="confirm-delete-report" type="danger" :loading="mutating" @click="confirmReportDeletion">确认删除</el-button></template></el-dialog>
  </section>
</template>
