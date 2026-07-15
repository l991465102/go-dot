<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useProjectStore } from '@/stores/project'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const projects = useProjectStore()
const form = reactive({ username: '', password: '' })
const loading = ref(false)
const error = ref('')

async function submit() {
  if (!form.username || !form.password || loading.value) return
  loading.value = true
  error.value = ''
  try {
    await auth.signIn(form.username, form.password)
  } catch {
    error.value = '账号或密码错误，请重新输入。'
    loading.value = false
    return
  }
  if (auth.mustChangePassword) {
    loading.value = false
    await router.replace({ name: 'change-password' })
    return
  }
  try {
    await projects.load()
  } catch {
    loading.value = false
    await router.replace({ name: 'project-bootstrap-error' })
    return
  }
  const redirect = typeof route.query.redirect === 'string' ? route.query.redirect : null
  if (redirect) await router.replace(redirect)
  else if (projects.currentProjectId) await router.replace({ name: 'project-overview', params: { projectId: projects.currentProjectId } })
  else await router.replace({ name: 'project-onboarding' })
  loading.value = false
}
</script>

<template>
  <main class="auth-page">
    <section class="auth-story" aria-hidden="true">
      <div class="auth-story__content">
        <span class="brand__mark brand__mark--large">G.</span>
        <p class="eyebrow eyebrow--light">BUSINESS INTELLIGENCE</p>
        <h1>让每一个业务动作<br>都清晰可见</h1>
        <p>统一管理埋点、实时观察趋势、用可靠的数据推动产品决策。</p>
      </div>
    </section>
    <section class="auth-panel">
      <form class="auth-card" aria-labelledby="login-title" @submit.prevent="submit">
        <p class="eyebrow">GO DOT CONSOLE</p>
        <h2 id="login-title">欢迎回来</h2>
        <p class="muted">登录业务统计管理后台</p>
        <label for="username">账号</label>
        <el-input id="username" v-model="form.username" autocomplete="username" placeholder="请输入账号" size="large" />
        <label for="password">密码</label>
        <el-input id="password" v-model="form.password" type="password" show-password autocomplete="current-password" placeholder="请输入密码" size="large" />
        <p v-if="error" class="form-error" role="alert">{{ error }}</p>
        <el-button native-type="submit" type="primary" size="large" :loading="loading" :disabled="!form.username || !form.password">
          登录
        </el-button>
        <p class="auth-help">首次部署请使用初始管理员账号登录，并立即修改密码。</p>
      </form>
    </section>
  </main>
</template>
