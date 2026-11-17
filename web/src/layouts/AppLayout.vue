<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useProjectStore } from '@/stores/project'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const projects = useProjectStore()
const mobileOpen = ref(false)

const navItems = computed(() => [
  { label: '项目概览', route: 'project-overview', marker: '概' },
  { label: '点位管理', route: 'points', marker: '点' },
  { label: '业务分类', route: 'categories', marker: '类' },
  { label: '统计分析', route: 'statistics', marker: '统' },
  { label: '自定义报表', route: 'custom-reports', marker: '报' },
  { label: '数据诊断', route: 'diagnostics', marker: '诊' },
  { label: '项目设置', route: 'project-settings', marker: '设' },
])

async function selectProject(value: number) {
  if (!projects.select(Number(value))) return
  mobileOpen.value = false
  await router.push({ name: 'project-overview', params: { projectId: value } })
}

async function signOut() {
  try {
    await auth.signOut()
    projects.reset()
    await router.replace({ name: 'login' })
  } catch {
    projects.error = '退出登录失败，会话仍然有效，请稍后重试。'
  }
}
</script>

<template>
  <div class="app-shell">
    <button class="mobile-menu-button" type="button" aria-label="打开导航" @click="mobileOpen = !mobileOpen">
      ☰
    </button>
    <aside class="sidebar" :class="{ 'sidebar--open': mobileOpen }">
      <div class="brand" aria-label="Go Dot 业务统计">
        <span class="brand__mark">G.</span>
        <span><strong>Go Dot</strong><small>业务统计平台</small></span>
      </div>
      <nav aria-label="项目导航">
        <RouterLink
          v-for="item in navItems"
          :key="item.route"
          class="nav-link"
          :class="{ 'nav-link--active': route.name === item.route }"
          :to="{ name: item.route, params: { projectId: projects.currentProjectId } }"
          @click="mobileOpen = false"
        >
          <span class="nav-link__marker" aria-hidden="true">{{ item.marker }}</span>
          {{ item.label }}
        </RouterLink>
      </nav>
      <div class="sidebar__foot">
        <span class="status-dot" /> 数据服务在线
      </div>
    </aside>

    <div v-if="mobileOpen" class="mobile-backdrop" aria-hidden="true" @click="mobileOpen = false" />
    <section class="workspace">
      <header class="topbar">
        <div>
          <p class="eyebrow">当前项目</p>
          <el-select
            :model-value="projects.currentProjectId"
            aria-label="选择项目"
            class="project-select"
            :loading="projects.loading"
            @update:model-value="selectProject"
          >
            <el-option v-for="project in projects.projects" :key="project.id" :label="project.name" :value="project.id" />
          </el-select>
        </div>
        <div class="user-area">
          <el-tag :type="projects.canMutate ? 'primary' : 'info'" effect="plain">
            {{ projects.canMutate ? '管理员' : '只读' }}
          </el-tag>
          <el-dropdown trigger="click">
            <button class="user-button" type="button" aria-label="用户菜单">
              <span class="avatar">{{ auth.user?.displayName?.slice(0, 1) || auth.user?.username.slice(0, 1) }}</span>
              <span class="user-copy"><strong>{{ auth.user?.displayName || auth.user?.username }}</strong><small>{{ auth.user?.username }}</small></span>
              <span aria-hidden="true">⌄</span>
            </button>
            <template #dropdown>
              <el-dropdown-menu><el-dropdown-item @click="signOut">退出登录</el-dropdown-item></el-dropdown-menu>
            </template>
          </el-dropdown>
        </div>
      </header>

      <main class="content" tabindex="-1">
        <div v-if="projects.error" class="page-error" role="alert">
          <strong>项目加载失败</strong><span>{{ projects.error }}</span>
          <el-button size="small" @click="projects.load(Number(route.params.projectId))">重试</el-button>
        </div>
        <RouterView v-else />
      </main>
    </section>
  </div>
</template>
