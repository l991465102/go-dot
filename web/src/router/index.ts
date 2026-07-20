import { createRouter, createWebHistory, type Router } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useProjectStore } from '@/stores/project'
import AppLayout from '@/layouts/AppLayout.vue'
import LoginView from '@/views/LoginView.vue'
import ChangePasswordView from '@/views/ChangePasswordView.vue'
import PointWorkbenchView from '@/views/PointWorkbenchView.vue'
import PointDetailView from '@/views/PointDetailView.vue'
import CategoryManagementView from '@/views/CategoryManagementView.vue'
import ProjectOnboardingView from '@/views/ProjectOnboardingView.vue'
import ProjectBootstrapErrorView from '@/views/ProjectBootstrapErrorView.vue'

export function createAppRouter(): Router {
  const router = createRouter({
    history: createWebHistory(),
    routes: [
      { path: '/login', name: 'login', component: LoginView, meta: { public: true } },
      { path: '/change-password', name: 'change-password', component: ChangePasswordView },
      { path: '/onboarding', name: 'project-onboarding', component: ProjectOnboardingView },
      { path: '/project-bootstrap-error', name: 'project-bootstrap-error', component: ProjectBootstrapErrorView },
      {
        path: '/projects/:projectId',
        component: AppLayout,
        children: [
          { path: 'overview', name: 'project-overview', component: () => import('@/views/ProjectOverviewView.vue') },
          { path: 'points', name: 'points', component: PointWorkbenchView },
          { path: 'points/:pointId', name: 'point-detail', component: PointDetailView, props: (route) => ({ projectId: Number(route.params.projectId), pointId: Number(route.params.pointId), canMutate: useProjectStore().canMutate }) },
          { path: 'categories', name: 'categories', component: CategoryManagementView },
          { path: 'statistics', name: 'statistics', component: () => import('@/views/StatisticsView.vue') },
          { path: 'reports', name: 'custom-reports', component: () => import('@/views/CustomReportsView.vue') },
          { path: 'diagnostics', name: 'diagnostics', component: () => import('@/views/DiagnosticsView.vue') },
          { path: 'settings', name: 'project-settings', component: () => import('@/views/ProjectSettingsView.vue') },
        ],
      },
      { path: '/:pathMatch(.*)*', redirect: '/login' },
    ],
  })

  router.beforeEach(async (to) => {
    const auth = useAuthStore()
    if (!auth.initialized) await auth.restore()
    if (to.meta.public) {
      if (auth.authenticated && auth.mustChangePassword) return { name: 'change-password' }
      if (auth.authenticated) {
        const projectStore = useProjectStore()
        if (projectStore.loadState === 'idle') {
          try { await projectStore.load() } catch { return { name: 'project-bootstrap-error' } }
        }
        if (projectStore.loadState === 'error') return { name: 'project-bootstrap-error' }
        return projectStore.currentProjectId
          ? { name: 'project-overview', params: { projectId: projectStore.currentProjectId } }
          : { name: 'project-onboarding' }
      }
      return true
    }
    if (!auth.authenticated) return { name: 'login', query: { redirect: to.fullPath } }
    if (auth.mustChangePassword && to.name !== 'change-password') return { name: 'change-password' }
    if (!auth.mustChangePassword && to.name === 'change-password') {
      const projectStore = useProjectStore()
      if (projectStore.loadState === 'idle') {
        try { await projectStore.load() } catch { return { name: 'project-bootstrap-error' } }
      }
      if (projectStore.loadState === 'error') return { name: 'project-bootstrap-error' }
      return projectStore.currentProjectId
        ? { name: 'project-overview', params: { projectId: projectStore.currentProjectId } }
        : { name: 'project-onboarding' }
    }
    if (to.name === 'project-onboarding') {
      const projectStore = useProjectStore()
      if (projectStore.loadState === 'idle') {
        try { await projectStore.load() } catch { return { name: 'project-bootstrap-error' } }
      }
      if (projectStore.loadState === 'error') return { name: 'project-bootstrap-error' }
      return projectStore.currentProjectId
        ? { name: 'project-overview', params: { projectId: projectStore.currentProjectId } }
        : true
    }
    if (to.name === 'project-bootstrap-error') {
      const projectStore = useProjectStore()
      if (projectStore.loadState === 'idle') {
        try { await projectStore.load() } catch { return true }
      }
      if (projectStore.loadState === 'error') return true
      return projectStore.currentProjectId
        ? { name: 'project-overview', params: { projectId: projectStore.currentProjectId } }
        : { name: 'project-onboarding' }
    }
    if (to.path.startsWith('/projects/')) {
      const projectStore = useProjectStore()
      const requestedId = Number(to.params.projectId)
      if (projectStore.loadState === 'idle') {
        try {
          await projectStore.load(requestedId)
        } catch {
          return { name: 'project-bootstrap-error' }
        }
      }
      if (projectStore.loadState === 'error') return { name: 'project-bootstrap-error' }
      if (!projectStore.select(requestedId)) {
        if (projectStore.currentProjectId) {
          return { name: 'project-overview', params: { projectId: projectStore.currentProjectId } }
        }
        return { name: 'project-onboarding' }
      }
    }
    return true
  })

  return router
}
