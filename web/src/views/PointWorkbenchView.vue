<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useProjectStore } from '@/stores/project'
import * as api from '@/api/tracking'
import type { Category, Point, PointFilters } from '@/api/tracking'
import PointWizard from '@/components/points/PointWizard.vue'

const route = useRoute()
const router = useRouter()
const projects = useProjectStore()
const projectId = computed(() => Number(route.params.projectId))
const canMutate = computed(() => projects.canMutate)
const categories = ref<Category[]>([])
const points = ref<Point[]>([])
const total = ref(0)
const loading = ref(false)
const error = ref('')
const selectedCategory = ref(0)
const wizard = ref(false)
const editing = ref<Point>()
const filters = ref<PointFilters>({ q: '', definition_state: '', development_state: '', behavior_type: '', platform: '', owner: '', created_from: '', created_to: '', page: 1, page_size: 20 })
let controller: AbortController | undefined

const domains = computed(() => categories.value.filter((c) => c.level === 'DOMAIN').sort((a, b) => a.sort_order - b.sort_order))
const children = (id: number) => categories.value.filter((c) => c.parent_id === id).sort((a, b) => a.sort_order - b.sort_order)

function edit(point?: Point) { editing.value = point; wizard.value = true }
function closeWizard() { wizard.value = false; editing.value = undefined }

async function openRouteDraft(signal: AbortSignal) {
  const raw = route.query.edit
  if (raw === undefined) return
  const value = Array.isArray(raw) ? raw[0] : raw
  const id = typeof value === 'string' && /^\d+$/.test(value) ? Number(value) : 0
  await router.replace({ query: { ...route.query, edit: undefined } })
  if (!canMutate.value || !Number.isSafeInteger(id) || id <= 0) { error.value = '无效的草稿编辑请求'; return }
  try {
    const draft = await api.getPoint(projectId.value, id, signal)
    if (draft.project_id !== projectId.value || draft.definition_state !== 'DRAFT') { error.value = '只能编辑当前项目中的草稿点位'; return }
    edit(draft)
  } catch (cause) {
    if ((cause as { name?: string }).name !== 'CanceledError') error.value = '草稿不存在或无权访问'
  }
}

async function load() {
  controller?.abort()
  const active = new AbortController()
  controller = active
  const signal = active.signal
  loading.value = true
  error.value = ''
  try {
    const [cats, list] = await Promise.all([
      api.listCategories(projectId.value, signal),
      api.listPoints(projectId.value, { ...filters.value, category_id: selectedCategory.value || undefined }, signal),
    ])
    categories.value = cats
    points.value = list.items
    total.value = list.total
    await openRouteDraft(signal)
  } catch (cause) {
    if ((cause as { name?: string }).name !== 'CanceledError') error.value = cause instanceof Error ? cause.message : '点位加载失败'
  } finally { if (controller === active) loading.value = false }
}

function reset() {
  filters.value = { q: '', definition_state: '', development_state: '', behavior_type: '', platform: '', owner: '', created_from: '', created_to: '', page: 1, page_size: 20 }
  selectedCategory.value = 0
  closeWizard()
  void load()
}
function selectCategory(id: number) { selectedCategory.value = id; filters.value.page = 1; void load() }
function goPage(page: number) { if (page < 1 || page > Math.ceil(total.value / (filters.value.page_size || 20))) return; filters.value.page = page; void load() }
async function saved() { closeWizard(); await load() }
async function published(id: number) { closeWizard(); await router.push({ name: 'point-detail', params: { projectId: projectId.value, pointId: id } }) }

watch(projectId, reset, { immediate: true })
onBeforeUnmount(() => controller?.abort())
</script>

<template>
  <section>
    <header class="page-heading">
      <div><p class="eyebrow">Tracking governance</p><h1>点位管理</h1><p>通过分类树浏览点位，统一定义、开发和发布口径。</p></div>
      <button v-if="canMutate" class="primary-button" type="button" @click="edit()">新增点位</button>
    </header>
    <div class="workbench">
      <aside class="category-tree" aria-label="业务分类树">
        <button :class="{ active: selectedCategory === 0 }" @click="selectCategory(0)">全部点位</button>
        <div v-for="domain in domains" :key="domain.id">
          <button :class="{ active: selectedCategory === domain.id, disabled: !domain.active }" @click="selectCategory(domain.id)">{{ domain.name }}</button>
          <div v-for="module in children(domain.id)" :key="module.id" class="tree-level">
            <button :class="{ active: selectedCategory === module.id, disabled: !module.active }" @click="selectCategory(module.id)">{{ module.name }}</button>
            <div v-for="scene in children(module.id)" :key="scene.id" class="tree-level">
              <button :class="{ active: selectedCategory === scene.id, disabled: !scene.active }" @click="selectCategory(scene.id)">{{ scene.name }}</button>
            </div>
          </div>
        </div>
      </aside>
      <main class="point-panel">
        <form class="filter-bar" @submit.prevent="filters.page = 1; load()">
          <input v-model="filters.q" aria-label="搜索点位" placeholder="搜索编码或名称">
          <select v-model="filters.definition_state" aria-label="定义状态"><option value="">全部定义状态</option><option>DRAFT</option><option>PUBLISHED</option><option>DEPRECATED</option></select>
          <select v-model="filters.development_state" aria-label="开发状态"><option value="">全部开发状态</option><option>PENDING</option><option>DEVELOPING</option><option>TESTING</option><option>ONLINE</option></select>
          <select v-model="filters.behavior_type" aria-label="行为类型"><option value="">全部行为</option><option v-for="v in ['PAGE_VIEW','ELEMENT_EXPOSE','ELEMENT_CLICK','BUSINESS_ACTION','BUSINESS_RESULT','CUSTOM']" :key="v">{{ v }}</option></select>
          <select v-model="filters.platform" aria-label="平台"><option value="">全部平台</option><option v-for="v in ['web','ios','android','mini_program','server']" :key="v">{{ v }}</option></select>
          <input v-model="filters.owner" aria-label="负责人" placeholder="负责人">
          <input v-model="filters.created_from" type="date" aria-label="创建开始日期">
          <input v-model="filters.created_to" type="date" aria-label="创建结束日期">
          <button class="secondary-button" type="submit">筛选</button>
        </form>
        <div v-if="loading" class="empty-state" aria-live="polite">正在加载点位…</div>
        <div v-else-if="error" class="form-alert" role="alert">{{ error }} <button type="button" @click="load">重试</button></div>
        <div v-else-if="!points.length" class="empty-state">暂无符合条件的点位</div>
        <div v-else class="table-wrap">
          <table><thead><tr><th>点位</th><th>分类</th><th>行为</th><th>定义状态</th><th>开发状态</th><th>平台</th><th>负责人</th><th></th></tr></thead><tbody>
            <tr v-for="p in points" :key="p.id"><td><strong>{{ p.name }}</strong><small>{{ p.code }}</small></td><td>{{ p.category_path.map(c => c.name).join(' / ') || p.category.name }}</td><td>{{ p.behavior_type }}</td><td><span class="state-chip">{{ p.definition_state }}</span></td><td>{{ p.development_state }}</td><td>{{ p.supported_platforms.join('、') }}</td><td>{{ p.product_owner || p.developer_owner || '-' }}</td><td><button class="text-link" @click="router.push({ name: 'point-detail', params: { projectId, pointId: p.id } })">查看</button><button v-if="canMutate && p.definition_state === 'DRAFT'" class="text-link" @click="edit(p)">编辑</button></td></tr>
          </tbody></table>
        </div>
        <footer class="list-footer"><span>共 {{ total }} 个点位</span><div v-if="total > (filters.page_size || 20)" class="pagination"><button class="secondary-button" :disabled="(filters.page || 1) <= 1" @click="goPage((filters.page || 1) - 1)">上一页</button><span>第 {{ filters.page }} / {{ Math.ceil(total / (filters.page_size || 20)) }} 页</span><button class="secondary-button" :disabled="(filters.page || 1) >= Math.ceil(total / (filters.page_size || 20))" @click="goPage((filters.page || 1) + 1)">下一页</button></div></footer>
      </main>
    </div>
    <div v-if="wizard" class="modal-backdrop" role="presentation"><div class="modal-card modal-card--wide" role="dialog" aria-modal="true" aria-label="点位编辑向导"><PointWizard :project-id="projectId" :categories="categories" :initial-point="editing" :can-mutate="canMutate" @cancel="closeWizard" @saved="saved" @published="published" /></div></div>
  </section>
</template>
