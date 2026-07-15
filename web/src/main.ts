import { createApp } from 'vue'
import { createPinia } from 'pinia'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import './styles.css'
import App from './App.vue'
import { createAppRouter } from './router'
import { setUnauthorizedHandler } from './api/client'
import { useAuthStore } from './stores/auth'
import { useProjectStore } from './stores/project'

const app = createApp(App)
const pinia = createPinia()
const router = createAppRouter()

app.use(pinia)
app.use(router)
app.use(ElementPlus)

setUnauthorizedHandler(() => {
  useAuthStore(pinia).clearSession()
  useProjectStore(pinia).reset()
  if (router.currentRoute.value.name !== 'login') void router.replace({ name: 'login' })
})

app.mount('#app')
