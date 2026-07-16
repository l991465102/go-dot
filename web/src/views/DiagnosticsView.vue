<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useProjectStore } from '@/stores/project'
import { listDeadLetters, listInvalidEvents, listRecentEvents, type DeadLetter, type InvalidEvent, type RecentEvent } from '@/api/diagnostics'
import InvalidEventTable from '@/components/diagnostics/InvalidEventTable.vue'
import DeadLetterTable from '@/components/diagnostics/DeadLetterTable.vue'

const projects = useProjectStore()
const projectId = computed(() => projects.currentProjectId)
const tab = ref<'invalid' | 'recent' | 'dead'>('invalid')
const filters = reactive({ point: '', platform: '', channel: '', errorCode: '' })
const invalid = ref<InvalidEvent[]>([])
const recent = ref<RecentEvent[]>([])
const dead = ref<DeadLetter[]>([])
const nextCursor = ref('')
const cursors = ref<string[]>([])
const loading = ref(false)
const error = ref('')
let controller: AbortController | undefined

function query(id: number, cursor = '') {
  const now = new Date()
  return {
    projectId: id,
    start: new Date(now.getTime() - 7 * 24 * 3_600_000).toISOString(),
    end: now.toISOString(),
    pointCodes: filters.point ? [filters.point] : undefined,
    platform: filters.platform,
    channel: filters.channel,
    errorCode: filters.errorCode,
    cursor,
    limit: 20,
  }
}

async function load(cursor = '') {
  controller?.abort()
  const active = new AbortController()
  controller = active
  const id = projectId.value
  if (!id) {
    loading.value = false
    return
  }

  loading.value = true
  error.value = ''
  try {
    if (tab.value === 'invalid') {
      const page = await listInvalidEvents(query(id, cursor), active.signal)
      if (controller !== active || active.signal.aborted || projectId.value !== id) return
      invalid.value = page.items
      nextCursor.value = page.next_cursor ?? ''
    } else if (tab.value === 'recent') {
      const page = await listRecentEvents(query(id, cursor), active.signal)
      if (controller !== active || active.signal.aborted || projectId.value !== id) return
      recent.value = page.items
      nextCursor.value = page.next_cursor ?? ''
    } else {
      const page = await listDeadLetters(id, cursor, 20, active.signal)
      if (controller !== active || active.signal.aborted || projectId.value !== id) return
      dead.value = page.items
      nextCursor.value = page.next_cursor ?? ''
    }
  } catch (cause) {
    if (controller === active && !active.signal.aborted && projectId.value === id) error.value = cause instanceof Error ? cause.message : '诊断查询失败'
  } finally {
    if (controller === active && !active.signal.aborted && projectId.value === id) loading.value = false
  }
}

function switchTab() { cursors.value = []; nextCursor.value = ''; load() }
function next() { if (nextCursor.value) { cursors.value.push(nextCursor.value); load(nextCursor.value) } }
function previous() { cursors.value.pop(); load(cursors.value.at(-1) ?? '') }
watch(projectId, () => { cursors.value = []; load() }, { immediate: true })
onBeforeUnmount(() => controller?.abort())
</script>

<template>
  <section aria-labelledby="diagnostics-title"><header class="page-heading"><div><p class="eyebrow">DATA QUALITY</p><h1 id="diagnostics-title">数据诊断</h1><p>仅展示脱敏事件与死信元数据；控制台不提供死信重放。</p></div></header>
    <el-tabs v-model="tab" @tab-change="switchTab"><el-tab-pane label="无效事件" name="invalid" /><el-tab-pane label="最近事件" name="recent" /><el-tab-pane label="死信" name="dead" /></el-tabs>
    <el-form v-if="tab !== 'dead'" class="filter-panel" @submit.prevent="switchTab"><el-form-item label="点位"><el-input v-model="filters.point" /></el-form-item><el-form-item label="平台"><el-input v-model="filters.platform" /></el-form-item><el-form-item label="渠道"><el-input v-model="filters.channel" /></el-form-item><el-form-item v-if="tab === 'invalid'" label="错误码"><el-input v-model="filters.errorCode" /></el-form-item><el-button native-type="submit" type="primary">筛选</el-button></el-form>
    <div v-if="error" class="page-error" role="alert">{{ error }}</div><div v-loading="loading"><InvalidEventTable v-if="tab === 'invalid'" :items="invalid" /><el-table v-else-if="tab === 'recent'" :data="recent" empty-text="暂无最近事件"><el-table-column prop="received_at" label="接收时间" /><el-table-column prop="point_code" label="点位" /><el-table-column prop="platform" label="平台" /><el-table-column prop="channel" label="渠道" /></el-table><DeadLetterTable v-else :items="dead" /></div>
    <footer class="pagination-actions"><el-button :disabled="!cursors.length" @click="previous">上一页</el-button><el-button :disabled="!nextCursor" @click="next">下一页</el-button></footer>
  </section>
</template>
