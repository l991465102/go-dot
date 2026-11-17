import { apiClient } from './client'
import type { Platform, DevelopmentState } from './tracking'
export interface RequirementInput { title: string; goal: string; scenario: string; requirement_url: string; due_date: string }
export interface Requirement extends RequirementInput { id: number; project_id: number; created_by: number; created_at: string }
export interface TaskInput { point_id: number; platform: Platform; assignee_id: number | null; state: DevelopmentState }
export interface Task extends TaskInput { id: number; requirement_id: number; point_name: string }
export async function listRequirements(p: number, beforeId?: number) { return (await apiClient.get<Requirement[]>(`/projects/${p}/requirements`, {params: beforeId ? {before_id:beforeId} : {}})).data }
export async function createRequirement(p: number, input: RequirementInput) { return (await apiClient.post<{id:number}>(`/projects/${p}/requirements`, input)).data }
export async function listTasks(p: number, id: number) { return (await apiClient.get<Task[]>(`/projects/${p}/requirements/${id}/tasks`)).data }
export async function saveTask(p: number, id: number, input: TaskInput, taskId?: number) { if (taskId) await apiClient.put(`/projects/${p}/requirements/${id}/tasks/${taskId}`,input); else await apiClient.post(`/projects/${p}/requirements/${id}/tasks`,input) }

export async function updateRequirement(p: number, id: number, input: RequirementInput) { await apiClient.put(`/projects/${p}/requirements/${id}`, input) }

export async function deleteTask(p: number, id: number, taskId: number) { await apiClient.delete(`/projects/${p}/requirements/${id}/tasks/${taskId}`) }
