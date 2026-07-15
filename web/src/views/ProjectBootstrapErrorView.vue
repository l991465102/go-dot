<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useProjectStore } from '@/stores/project'

const router = useRouter()
const auth = useAuthStore()
const projects = useProjectStore()
const retrying = ref(false)
const retryError = ref('')

async function retry() {
  if (retrying.value) return
  retrying.value = true
  retryError.value = ''
  try {
    await projects.load()
    if (projects.currentProjectId) await router.replace({ name: 'project-overview', params: { projectId: projects.currentProjectId } })
    else await router.replace({ name: 'project-onboarding' })
  } catch {
    retryError.value = '仍然无法加载项目权限，请稍后重试。'
  } finally {
    retrying.value = false
  }
}

async function signOut() {
  try {
    await auth.signOut()
    projects.reset()
    await router.replace({ name: 'login' })
  } catch {
    retryError.value = '退出失败，会话仍然有效，请稍后重试。'
  }
}
</script>

<template>
  <main class="onboarding-page">
    <header class="onboarding-topbar">
      <div class="brand"><span class="brand__mark">G.</span><span><strong>Go Dot</strong><small>业务统计平台</small></span></div>
      <button type="button" class="text-button" @click="signOut">退出登录</button>
    </header>
    <section class="bootstrap-error-card" aria-labelledby="bootstrap-error-title">
      <span class="error-mark" aria-hidden="true">!</span>
      <p class="eyebrow">PROJECT ACCESS</p>
      <h1 id="bootstrap-error-title">暂时无法加载项目权限</h1>
      <p class="muted">登录状态仍然有效，但项目成员信息加载失败。为保护数据权限，在确认项目列表前不会开放项目创建或管理功能。</p>
      <p v-if="retryError" class="form-error" role="alert">{{ retryError }}</p>
      <el-button type="primary" size="large" :loading="retrying" @click="retry">重新加载项目</el-button>
    </section>
  </main>
</template>
