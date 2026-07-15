import { apiClient } from './client'

export interface User {
  id: number
  username: string
  displayName: string
  mustChangePassword: boolean
  status: string
}

interface ApiUser {
  id: number
  username: string
  display_name: string
  must_change_password: boolean
  status: string
}

interface ApiSession {
  access_token: string
  expires_in: number
  must_change_password: boolean
  user: ApiUser
}

export interface Session {
  accessToken: string
  expiresIn?: number
  mustChangePassword: boolean
  user: User
}

function mapUser(user: ApiUser): User {
  return {
    id: user.id,
    username: user.username,
    displayName: user.display_name,
    mustChangePassword: user.must_change_password,
    status: user.status,
  }
}

function mapSession(session: ApiSession): Session {
  return {
    accessToken: session.access_token,
    expiresIn: session.expires_in,
    mustChangePassword: session.must_change_password,
    user: mapUser(session.user),
  }
}

export async function login(username: string, password: string) {
  const { data } = await apiClient.post<ApiSession>('/auth/login', { username, password })
  return mapSession(data)
}

export async function refresh() {
  const { data } = await apiClient.post<ApiSession>('/auth/refresh')
  return mapSession(data)
}

export async function logout() {
  await apiClient.post('/auth/logout')
}

export async function changePassword(currentPassword: string, newPassword: string) {
  await apiClient.post('/auth/change-password', {
    current_password: currentPassword,
    new_password: newPassword,
  })
}
