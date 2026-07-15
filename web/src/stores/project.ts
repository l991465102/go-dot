import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import { createProject, listProjects, type CreateProjectInput, type Project } from '@/api/projects'

const storageKey = 'go-dot:last-project-id'

export const useProjectStore = defineStore('project', () => {
  const projects = ref<Project[]>([])
  const currentProjectId = ref<number | null>(null)
  const loading = ref(false)
  const error = ref<string | null>(null)

  const currentProject = computed(() => projects.value.find((item) => item.id === currentProjectId.value) ?? null)
  const currentRole = computed(() => currentProject.value?.role ?? null)
  const canMutate = computed(() => currentRole.value === 'ADMIN')

  function select(projectId: number) {
    if (!projects.value.some((item) => item.id === projectId)) return false
    currentProjectId.value = projectId
    localStorage.setItem(storageKey, String(projectId))
    return true
  }

  async function load(preferredId?: number) {
    loading.value = true
    error.value = null
    try {
      projects.value = await listProjects()
      const stored = Number(localStorage.getItem(storageKey))
      const next = [preferredId, stored, projects.value[0]?.id].find(
        (candidate): candidate is number => Number.isSafeInteger(candidate) && projects.value.some((item) => item.id === candidate),
      )
      if (next !== undefined) select(next)
      else currentProjectId.value = null
    } catch (cause) {
      error.value = cause instanceof Error ? cause.message : '项目加载失败'
      throw cause
    } finally {
      loading.value = false
    }
  }

  async function create(input: CreateProjectInput) {
    loading.value = true
    error.value = null
    try {
      const created = await createProject(input)
      projects.value.push(created.project)
      select(created.project.id)
      return created
    } catch (cause) {
      error.value = cause instanceof Error ? cause.message : '项目创建失败'
      throw cause
    } finally {
      loading.value = false
    }
  }

  function reset() {
    projects.value = []
    currentProjectId.value = null
    error.value = null
  }

  return { projects, currentProjectId, currentProject, currentRole, canMutate, loading, error, load, create, select, reset }
})
