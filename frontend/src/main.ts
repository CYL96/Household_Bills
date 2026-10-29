import { createApp } from 'vue'
import ElementPlus from 'element-plus'
import zhCn from 'element-plus/es/locale/lang/zh-cn'
import 'element-plus/dist/index.css'
import App from './App.vue'
import router from './router'
import { bootstrapAuth } from './stores/auth'
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
  app.use(router)
  await router.isReady()
  app.mount('#app')
}

void startApp()
