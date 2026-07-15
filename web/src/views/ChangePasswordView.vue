<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useProjectStore } from '@/stores/project'

const router = useRouter()
const auth = useAuthStore()
const projects = useProjectStore()
const form = reactive({ currentPassword: '', newPassword: '', confirmPassword: '' })
const loading = ref(false)
const error = ref('')
const valid = computed(() => form.currentPassword.length > 0 && form.newPassword.length >= 8 && form.newPassword === form.confirmPassword)

async function submit() {
  if (!valid.value || loading.value) return
  loading.value = true
  error.value = ''
  try {
    await auth.updatePassword(form.currentPassword, form.newPassword)
    await projects.load()
    if (projects.currentProjectId) await router.replace({ name: 'project-overview', params: { projectId: projects.currentProjectId } })
    else await router.replace({ name: 'login' })
  } catch {
    error.value = '密码修改失败，请确认当前密码和新密码规则。'
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <main class="auth-page auth-page--compact">
    <section class="auth-panel">
      <form class="auth-card" aria-labelledby="password-title" @submit.prevent="submit">
        <span class="brand__mark">G.</span>
        <p class="eyebrow">账号安全</p>
        <h1 id="password-title">设置新密码</h1>
        <p class="muted">初始密码仅用于首次登录，完成修改后才能进入项目。</p>
        <label for="current-password">当前密码</label>
        <el-input id="current-password" v-model="form.currentPassword" type="password" show-password autocomplete="current-password" />
        <label for="new-password">新密码</label>
        <el-input id="new-password" v-model="form.newPassword" type="password" show-password autocomplete="new-password" />
        <small class="field-help">至少 8 个字符，请勿继续使用初始密码。</small>
        <label for="confirm-password">确认新密码</label>
        <el-input id="confirm-password" v-model="form.confirmPassword" type="password" show-password autocomplete="new-password" />
        <p v-if="form.confirmPassword && form.newPassword !== form.confirmPassword" class="form-error" role="alert">两次输入的密码不一致。</p>
        <p v-if="error" class="form-error" role="alert">{{ error }}</p>
        <el-button native-type="submit" type="primary" size="large" :loading="loading" :disabled="!valid">保存并进入系统</el-button>
      </form>
    </section>
  </main>
</template>
