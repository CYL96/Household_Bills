import { createApp, nextTick } from 'vue'
import ElementPlus from 'element-plus'
import zhCn from 'element-plus/es/locale/lang/zh-cn'
import 'element-plus/dist/index.css'
import App from './App.vue'
import router from './router'
import { bootstrapAuth } from './stores/auth'
import { ensureCategories } from './stores/categories'
import './styles/main.css'

const app = createApp(App)
app.use(ElementPlus, { locale: zhCn })

async function startApp(): Promise<void> {
  // Resolve the session before the router guard performs its initial navigation.
  try {
    await bootstrapAuth()
  } catch {
    // bootstrapAuth records failures in authState.bootError; keep the app usable.
  }
  // Preload the shared category snapshot; dialogs still force a refresh when
  // they open, and the store ignores responses that outlive their identity.
  await nextTick()
  void ensureCategories()
  app.use(router)
  await router.isReady()
  app.mount('#app')
}

void startApp()
