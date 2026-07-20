<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useProjectStore } from '@/stores/project'
import {
  addProjectMember,
  getWriteKeyStatus,
  listProjectMembers,
  removeProjectMember,
  rotateWriteKey,
  updateProjectSettings,
  type ProjectMember,
  type ProjectRole,
  type WriteKeyStatus,
} from '@/api/projects'
import { DISPLAY_TIMEZONE, formatShanghaiDateTime } from '@/utils/datetime'

const projects = useProjectStore()
const project = computed(() => projects.currentProject)
const members = ref<ProjectMember[]>([])
const keyStatus = ref<WriteKeyStatus>({ previous_key_active: false })
const plaintextKey = ref('')
const rotationConfirmOpen = ref(false)
const loading = ref(false)
const error = ref('')
const form = reactive({ timezone: '', origins: '' })
const memberForm = reactive({ username: '', role: 'VIEWER' as ProjectRole })
let controller: AbortController | undefined
let contextVersion = 0

function owns(active: AbortController, projectId: number) {
  return controller === active && !active.signal.aborted && project.value?.id === projectId
}

async function load() {
  contextVersion += 1
  controller?.abort()
  const active = new AbortController()
  controller = active
  plaintextKey.value = ''
  members.value = []
  keyStatus.value = { previous_key_active: false }

  const current = project.value
  if (!current) {
    loading.value = false
    return
  }

  loading.value = true
  error.value = ''
  form.timezone = current.timezone
  form.origins = current.allowedOrigins.join('\n')
  try {
    const [nextMembers, nextKeyStatus] = await Promise.all([
      listProjectMembers(current.id, active.signal),
      getWriteKeyStatus(current.id, active.signal),
    ])
    if (!owns(active, current.id)) return
    members.value = nextMembers
    keyStatus.value = nextKeyStatus
  } catch (cause) {
    if (owns(active, current.id)) error.value = cause instanceof Error ? cause.message : '设置加载失败'
  } finally {
    if (owns(active, current.id)) loading.value = false
  }
}

async function save() {
  const current = project.value
  if (!current || !projects.canMutate) return
  loading.value = true
  try {
    const updated = await updateProjectSettings(current.id, {
      timezone: form.timezone,
      allowedOrigins: form.origins.split('\n').map((value) => value.trim()).filter(Boolean),
    })
    if (project.value?.id !== current.id) return
    const index = projects.projects.findIndex((value) => value.id === updated.id)
    const existing = index >= 0 ? projects.projects[index] : undefined
    if (existing) projects.projects[index] = { ...updated, role: existing.role }
  } catch (cause) {
    if (project.value?.id === current.id) error.value = cause instanceof Error ? cause.message : '保存失败'
  } finally {
    if (project.value?.id === current.id) loading.value = false
  }
}

async function add() {
  const current = project.value
  if (!current || !projects.canMutate) return
  error.value = ''
  try {
    await addProjectMember(current.id, memberForm.username.trim(), memberForm.role)
    if (project.value?.id !== current.id) return
    memberForm.username = ''
    await load()
  } catch (cause) {
    if (project.value?.id === current.id) error.value = cause instanceof Error ? cause.message : '成员保存失败'
  }
}

async function remove(member: ProjectMember) {
  const current = project.value
  if (!current || !projects.canMutate) return
  error.value = ''
  try {
    await removeProjectMember(current.id, member.user_id)
    if (project.value?.id === current.id) await load()
  } catch (cause) {
    if (project.value?.id === current.id) error.value = cause instanceof Error ? cause.message : '成员移除失败'
  }
}

async function rotate() {
  const current = project.value
  if (!current || !projects.canMutate) return
  const context = contextVersion
  error.value = ''
  plaintextKey.value = ''
  try {
    const result = await rotateWriteKey(current.id)
    if (contextVersion !== context || project.value?.id !== current.id) return
    plaintextKey.value = result.write_key
    keyStatus.value = { previous_key_active: true, previous_key_expires_at: result.previous_key_expires_at }
  } catch (cause) {
    if (contextVersion === context && project.value?.id === current.id) error.value = cause instanceof Error ? cause.message : '密钥轮换失败'
  }
}

function confirmRotation() {
  rotationConfirmOpen.value = false
  void rotate()
}

watch(() => projects.currentProjectId, load, { immediate: true })
onBeforeUnmount(() => controller?.abort())
</script>

<template>
  <section aria-labelledby="settings-title">
    <header class="page-heading">
      <div><p class="eyebrow">PROJECT SETTINGS</p><h1 id="settings-title">项目设置</h1><p>管理统计时区、Web Origin、成员与写入密钥。</p></div>
    </header>
    <div v-if="error" class="page-error" role="alert">{{ error }}</div>

    <article class="overview-card">
      <h2>基础设置</h2>
      <el-form label-position="top" @submit.prevent="save">
        <el-form-item label="项目时区"><el-input v-model="form.timezone" :readonly="!projects.canMutate" /></el-form-item>
        <el-form-item label="允许的 Web Origins（每行一个）"><el-input v-model="form.origins" type="textarea" :rows="4" :readonly="!projects.canMutate" /></el-form-item>
        <el-button v-if="projects.canMutate" data-test="save-settings" native-type="submit" type="primary" :loading="loading">保存设置</el-button>
      </el-form>
    </article>

    <article class="overview-card">
      <h2>项目成员</h2>
      <el-form v-if="projects.canMutate" data-test="member-mutation-form" class="inline-settings" @submit.prevent="add">
        <el-input v-model="memberForm.username" placeholder="已存在账号用户名" />
        <el-select v-model="memberForm.role"><el-option label="管理员" value="ADMIN" /><el-option label="只读" value="VIEWER" /></el-select>
        <el-button native-type="submit" type="primary" :disabled="!memberForm.username.trim()">添加或更新</el-button>
      </el-form>
      <el-table :data="members">
        <el-table-column prop="username" label="用户名" /><el-table-column prop="display_name" label="显示名" /><el-table-column prop="role" label="角色" />
        <el-table-column v-if="projects.canMutate" label="操作"><template #default="scope"><el-button data-test="remove-member" type="danger" link @click="remove(scope.row)">移除</el-button></template></el-table-column>
      </el-table>
    </article>

    <article class="overview-card">
      <h2>写入密钥</h2>
      <el-alert v-if="keyStatus.previous_key_active" type="warning" :closable="false" :title="`旧密钥仍可使用至 ${formatShanghaiDateTime(keyStatus.previous_key_expires_at)}（${DISPLAY_TIMEZONE}，轮换后最多 24 小时）`" />
      <p v-else>当前没有处于宽限期的旧密钥。</p>
      <p>当前写入密钥永久有效，不会自动轮换。仅在确认密钥泄露时执行紧急轮换。</p>
      <p>服务端只保存哈希，当前密钥无法再次查看。轮换后新密钥只显示一次。</p>
      <el-button v-if="projects.canMutate" data-test="rotate-write-key" type="danger" @click="rotationConfirmOpen = true">紧急轮换写入密钥</el-button>
      <div v-if="plaintextKey" class="write-key-box" role="status"><strong>请立即保存，新密钥关闭页面后不再显示</strong><code>{{ plaintextKey }}</code><el-button @click="plaintextKey = ''">已保存并隐藏</el-button></div>
    </article>

    <el-dialog v-model="rotationConfirmOpen" title="确认紧急轮换写入密钥" width="520px" :close-on-click-modal="false">
      <p>轮换后所有上报端都需要切换到新密钥。旧密钥仅保留 24 小时，请确认已准备好协调各端更新。</p>
      <template #footer>
        <el-button data-test="cancel-key-rotation" @click="rotationConfirmOpen = false">取消</el-button>
        <el-button data-test="confirm-key-rotation" type="danger" @click="confirmRotation">确认紧急轮换</el-button>
      </template>
    </el-dialog>
  </section>
</template>
