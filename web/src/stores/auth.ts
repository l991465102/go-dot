import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import * as authApi from '@/api/auth'
import { setAccessToken } from '@/api/client'
import type { Session, User } from '@/api/auth'

export const useAuthStore = defineStore('auth', () => {
  const user = ref<User | null>(null)
  const mustChangePassword = ref(false)
  const initialized = ref(false)
  const authenticated = computed(() => user.value !== null)

  function setSession(session: Session) {
    user.value = session.user
    mustChangePassword.value = session.mustChangePassword
    setAccessToken(session.accessToken)
    initialized.value = true
  }

  function clearSession() {
    user.value = null
    mustChangePassword.value = false
    setAccessToken(null)
    initialized.value = true
  }

  async function signIn(username: string, password: string) {
    setSession(await authApi.login(username, password))
  }

  async function restore() {
    if (initialized.value) return authenticated.value
    try {
      setSession(await authApi.refresh())
      return true
    } catch {
      clearSession()
      return false
    }
  }

  async function signOut() {
    await authApi.logout()
    clearSession()
  }

  async function updatePassword(currentPassword: string, newPassword: string) {
    await authApi.changePassword(currentPassword, newPassword)
    mustChangePassword.value = false
    if (user.value) user.value = { ...user.value, mustChangePassword: false }
  }

  return {
    user,
    mustChangePassword,
    initialized,
    authenticated,
    setSession,
    clearSession,
    signIn,
    restore,
    signOut,
    updatePassword,
  }
})
