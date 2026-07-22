<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useProjectStore } from '@/stores/project'
import { timezoneOptions } from '@/constants/enum-options'

const router = useRouter()
const auth = useAuthStore()
const projects = useProjectStore()
const form = reactive({ code: '', name: '', timezone: 'Asia/Shanghai', origins: '' })
const writeKey = ref('')
const error = ref('')
const codeValid = computed(() => /^[a-z][a-z0-9_-]{1,63}$/.test(form.code))
const valid = computed(() => codeValid.value && form.name.trim().length > 0)

async function create() {
  if (!valid.value || projects.loading || !projects.canCreateInitialProject) return
  error.value = ''
  try {
    const created = await projects.create({
      code: form.code,
      name: form.name.trim(),
      timezone: form.timezone,
      allowedOrigins: form.origins.split('\n').map((item) => item.trim()).filter(Boolean),
    })
    writeKey.value = created.writeKey
  } catch {
    error.value = '项目创建失败。请检查项目编码是否已存在，然后重试。'
  }
}

async function enterProject() {
  if (!projects.currentProjectId) return
  await router.replace({ name: 'project-overview', params: { projectId: projects.currentProjectId } })
}

async function signOut() {
  try {
    await auth.signOut()
    projects.reset()
    await router.replace({ name: 'login' })
  } catch {
    error.value = '退出失败，会话仍然有效，请稍后重试。'
  }
}
</script>

<template>
  <main class="onboarding-page">
    <header class="onboarding-topbar">
      <div class="brand"><span class="brand__mark">G.</span><span><strong>Go Dot</strong><small>业务统计平台</small></span></div>
      <button type="button" class="text-button" @click="signOut">退出登录</button>
    </header>
    <section class="onboarding-card" aria-labelledby="onboarding-title">
      <template v-if="!writeKey">
        <p class="eyebrow">GETTING STARTED</p>
        <h1 id="onboarding-title">创建第一个项目</h1>
        <p class="muted">项目用于隔离点位、上报密钥和统计数据。创建后，你将成为该项目的管理员。</p>
        <form @submit.prevent="create">
          <label for="project-name">项目名称</label>
          <el-input id="project-name" v-model="form.name" placeholder="例如：商城主站" size="large" />
          <label for="project-code">项目编码</label>
          <el-input id="project-code" v-model="form.code" placeholder="例如：mall" size="large" />
          <small class="field-help">小写字母开头，可使用数字、下划线和短横线。</small>
          <label for="project-timezone">统计时区</label>
          <el-select id="project-timezone" v-model="form.timezone" size="large">
            <el-option v-for="option in timezoneOptions" :key="option.value" :label="option.label" :value="option.value" />
          </el-select>
          <label for="allowed-origins">允许上报的 Web 域名（可选）</label>
          <el-input id="allowed-origins" v-model="form.origins" type="textarea" :rows="3" placeholder="每行一个，例如 https://www.example.com" />
          <p v-if="error" class="form-error" role="alert">{{ error }}</p>
          <el-button native-type="submit" type="primary" size="large" :loading="projects.loading" :disabled="!valid">创建项目</el-button>
        </form>
      </template>
      <template v-else>
        <span class="success-mark" aria-hidden="true">✓</span>
        <p class="eyebrow">PROJECT CREATED</p>
        <h1>项目创建成功</h1>
        <p class="muted">写入密钥仅展示一次，请立即保存到客户端的安全配置中。</p>
        <div class="write-key-box"><code>{{ writeKey }}</code></div>
        <el-button type="primary" size="large" @click="enterProject">我已保存，进入项目</el-button>
      </template>
    </section>
  </main>
</template>
