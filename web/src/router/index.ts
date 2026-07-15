import { createRouter, createWebHistory, type Router } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useProjectStore } from '@/stores/project'
import AppLayout from '@/layouts/AppLayout.vue'
import LoginView from '@/views/LoginView.vue'
import ChangePasswordView from '@/views/ChangePasswordView.vue'
import ProjectOverviewView from '@/views/ProjectOverviewView.vue'

const EmptyView = { template: '<div />' }

export function createAppRouter(): Router {
  const router = createRouter({
    history: createWebHistory(),
    routes: [
      { path: '/login', name: 'login', component: LoginView, meta: { public: true } },
      { path: '/change-password', name: 'change-password', component: ChangePasswordView },
      {
        path: '/projects/:projectId',
        component: AppLayout,
        children: [
          { path: 'overview', name: 'project-overview', component: ProjectOverviewView },
          { path: 'points', name: 'points', component: EmptyView },
          { path: 'categories', name: 'categories', component: EmptyView },
          { path: 'statistics', name: 'statistics', component: EmptyView },
          { path: 'diagnostics', name: 'diagnostics', component: EmptyView },
          { path: 'settings', name: 'project-settings', component: EmptyView },
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
      const lastProjectId = Number(localStorage.getItem('go-dot:last-project-id'))
      if (auth.authenticated && Number.isSafeInteger(lastProjectId) && lastProjectId > 0) {
        return { name: 'project-overview', params: { projectId: lastProjectId } }
      }
      return true
    }
    if (!auth.authenticated) return { name: 'login', query: { redirect: to.fullPath } }
    if (auth.mustChangePassword && to.name !== 'change-password') return { name: 'change-password' }
    if (!auth.mustChangePassword && to.name === 'change-password') {
      const lastProjectId = Number(localStorage.getItem('go-dot:last-project-id'))
      return Number.isSafeInteger(lastProjectId) && lastProjectId > 0
        ? { name: 'project-overview', params: { projectId: lastProjectId } }
        : { name: 'login' }
    }
    if (to.path.startsWith('/projects/')) {
      const projectStore = useProjectStore()
      const requestedId = Number(to.params.projectId)
      if (!projectStore.projects.length) {
        try {
          await projectStore.load(requestedId)
        } catch {
          return true
        }
      }
      if (!projectStore.select(requestedId)) {
        if (projectStore.currentProjectId) {
          return { name: 'project-overview', params: { projectId: projectStore.currentProjectId } }
        }
        return { name: 'login' }
      }
    }
    return true
  })

  return router
}
